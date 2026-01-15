// Package http provides a ByteSource backed by HTTP range requests.
package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"strconv"
	"strings"
)

// Source implements random access reads via HTTP range requests.
// It satisfies blob.ByteSource (io.ReaderAt plus Size).
type Source struct {
	url          string
	client       *nethttp.Client
	headers      nethttp.Header
	size         int64
	etag         string
	lastModified string
}

// Option configures a Source.
type Option func(*Source)

// WithClient sets the HTTP client used for requests.
func WithClient(client *nethttp.Client) Option {
	return func(s *Source) {
		s.client = client
	}
}

// WithHeaders sets additional headers on each request.
func WithHeaders(headers nethttp.Header) Option {
	return func(s *Source) {
		if headers == nil {
			return
		}
		s.headers = headers.Clone()
	}
}

// WithHeader sets a single header on each request.
func WithHeader(key, value string) Option {
	return func(s *Source) {
		if s.headers == nil {
			s.headers = make(nethttp.Header)
		}
		s.headers.Set(key, value)
	}
}

// NewSource creates a Source backed by HTTP range requests.
// It probes the remote to determine the content size.
func NewSource(url string, opts ...Option) (*Source, error) {
	s := &Source{
		url:    url,
		client: nethttp.DefaultClient,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.client == nil {
		s.client = nethttp.DefaultClient
	}

	size, etag, lastModified, err := s.fetchMetadata()
	if err != nil {
		return nil, err
	}
	s.size = size
	s.etag = etag
	s.lastModified = lastModified
	return s, nil
}

// Size returns the total size of the remote content.
func (s *Source) Size() int64 {
	return s.size
}

// ReadRange returns a reader for the specified byte range.
func (s *Source) ReadRange(off, length int64) (io.ReadCloser, error) {
	if length < 0 {
		return nil, fmt.Errorf("read range length %d: negative length", length)
	}
	if length == 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if off < 0 {
		return nil, fmt.Errorf("read range %d: negative offset", off)
	}
	if off >= s.size {
		return io.NopCloser(bytes.NewReader(nil)), io.EOF
	}
	if length > s.size-off {
		length = s.size - off
	}

	end := off + length - 1
	req, err := s.newRequest(nethttp.MethodGet)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case nethttp.StatusPartialContent:
		// ok
	case nethttp.StatusRequestedRangeNotSatisfiable:
		resp.Body.Close()
		return io.NopCloser(bytes.NewReader(nil)), io.EOF
	case nethttp.StatusOK:
		resp.Body.Close()
		return nil, errors.New("range requests not supported")
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("range request failed: %s", resp.Status)
	}

	return &rangeReadCloser{
		body:   resp.Body,
		reader: io.LimitReader(resp.Body, length),
	}, nil
}

// ReadAt reads data from the remote at the given offset using HTTP range requests.
func (s *Source) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("read at %d: negative offset", off)
	}
	if off >= s.size {
		return 0, io.EOF
	}

	end := off + int64(len(p)) - 1
	expected := len(p)
	if end >= s.size {
		end = s.size - 1
		expected = int(end - off + 1)
	}

	req, err := s.newRequest(nethttp.MethodGet)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case nethttp.StatusPartialContent:
		// ok
	case nethttp.StatusRequestedRangeNotSatisfiable:
		return 0, io.EOF
	case nethttp.StatusOK:
		return 0, errors.New("range requests not supported")
	default:
		return 0, fmt.Errorf("range request failed: %s", resp.Status)
	}

	n, err := io.ReadFull(resp.Body, p[:expected])
	if err != nil {
		return n, err
	}
	if expected < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (s *Source) fetchMetadata() (int64, string, string, error) {
	size := int64(-1)
	etag := ""
	lastModified := ""

	if resp, err := s.doHead(); err == nil {
		size = resp.ContentLength
		etag = resp.Header.Get("ETag")
		lastModified = resp.Header.Get("Last-Modified")
		resp.Body.Close()
	}

	rangeSize, rangeETag, rangeLastModified, err := s.rangeProbe()
	if err != nil {
		return 0, "", "", err
	}
	if size > 0 && size != rangeSize {
		return 0, "", "", fmt.Errorf("content size mismatch: head=%d range=%d", size, rangeSize)
	}
	if etag == "" {
		etag = rangeETag
	}
	if lastModified == "" {
		lastModified = rangeLastModified
	}
	return rangeSize, etag, lastModified, nil
}

func (s *Source) rangeProbe() (int64, string, string, error) {
	req, err := s.newRequest(nethttp.MethodGet)
	if err != nil {
		return 0, "", "", err
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != nethttp.StatusPartialContent {
		if resp.StatusCode == nethttp.StatusOK {
			return 0, "", "", errors.New("range requests not supported")
		}
		return 0, "", "", fmt.Errorf("range probe failed: %s", resp.Status)
	}

	crange := resp.Header.Get("Content-Range")
	if crange == "" {
		return 0, "", "", errors.New("range probe missing Content-Range")
	}
	size, err := parseContentRange(crange)
	if err != nil {
		return 0, "", "", err
	}

	return size, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}

func (s *Source) doHead() (*nethttp.Response, error) {
	req, err := s.newRequest(nethttp.MethodHead)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req)
}

func (s *Source) newRequest(method string) (*nethttp.Request, error) {
	req, err := nethttp.NewRequest(method, s.url, nil)
	if err != nil {
		return nil, err
	}
	for key, values := range s.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	if method == nethttp.MethodGet {
		if s.etag != "" && req.Header.Get("If-Match") == "" {
			req.Header.Set("If-Match", s.etag)
		}
		if s.lastModified != "" && req.Header.Get("If-Unmodified-Since") == "" {
			req.Header.Set("If-Unmodified-Since", s.lastModified)
		}
	}
	return req, nil
}

type rangeReadCloser struct {
	body   io.ReadCloser
	reader io.Reader
}

func (r *rangeReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *rangeReadCloser) Close() error {
	_, _ = io.Copy(io.Discard, r.body)
	return r.body.Close()
}

func parseContentRange(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "bytes "), "/", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	if parts[1] == "*" {
		return 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	if size < 0 {
		return 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	return size, nil
}
