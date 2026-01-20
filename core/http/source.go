package http //nolint:revive // intentional naming for domain clarity

import (
	"bytes"
	"context"
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
	url                   string
	client                *nethttp.Client
	headers               nethttp.Header
	size                  int64
	etag                  string
	lastModified          string
	sourceID              string
	useConditionalHeaders bool
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

// WithSourceID overrides the default source identifier used for caching.
func WithSourceID(id string) Option {
	return func(s *Source) {
		s.sourceID = id
	}
}

// WithConditionalHeaders enables conditional range reads using ETag or Last-Modified.
// This is disabled by default because some registries reject conditional range requests.
func WithConditionalHeaders() Option {
	return func(s *Source) {
		s.useConditionalHeaders = true
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
	if s.sourceID == "" {
		s.sourceID = s.defaultSourceID()
	}
	return s, nil
}

// Size returns the total size of the remote content.
func (s *Source) Size() int64 {
	return s.size
}

// SourceID returns a stable identifier for the remote content.
func (s *Source) SourceID() string {
	return s.sourceID
}

// ReadRange returns a reader for the specified byte range [off, off+length).
// It returns an error if offset or length is negative. If the offset is at or
// beyond the content size, it returns io.EOF. The returned reader must be closed
// by the caller to release the underlying HTTP connection.
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
	resp, err := s.rangeRequest(off, end, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == nethttp.StatusPreconditionFailed && s.hasConditionalHeaders() {
		resp.Body.Close()
		resp, err = s.rangeRequest(off, end, false)
		if err != nil {
			return nil, err
		}
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

// ReadAt reads len(p) bytes from the remote at the given offset using HTTP range requests.
// It implements [io.ReaderAt]. If fewer bytes are available than requested, it returns
// the number of bytes read along with io.EOF.
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

	resp, err := s.rangeRequest(off, end, true)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == nethttp.StatusPreconditionFailed && s.hasConditionalHeaders() {
		resp.Body.Close()
		resp, err = s.rangeRequest(off, end, false)
		if err != nil {
			return 0, err
		}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // best-effort drain for connection reuse
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

// defaultSourceID builds a source identifier from the URL and available metadata.
func (s *Source) defaultSourceID() string {
	if s.etag != "" {
		return fmt.Sprintf("url:%s|etag:%s", s.url, s.etag)
	}
	if s.lastModified != "" {
		return fmt.Sprintf("url:%s|mod:%s|size:%d", s.url, s.lastModified, s.size)
	}
	return fmt.Sprintf("url:%s|size:%d", s.url, s.size)
}

// fetchMetadata retrieves content size and cache validators from the remote server.
// It first attempts a HEAD request, then verifies with a range probe.
func (s *Source) fetchMetadata() (size int64, etag, lastModified string, err error) {
	size = -1

	if resp, headErr := s.doHead(); headErr == nil {
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

// rangeProbe verifies range request support and extracts content size from Content-Range.
func (s *Source) rangeProbe() (size int64, etag, lastModified string, err error) {
	req, err := s.newRequest(nethttp.MethodGet, false)
	if err != nil {
		return 0, "", "", err
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // best-effort drain for connection reuse
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
	size, err = parseContentRange(crange)
	if err != nil {
		return 0, "", "", err
	}

	return size, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}

// doHead performs a HEAD request to retrieve metadata without body content.
func (s *Source) doHead() (*nethttp.Response, error) {
	req, err := s.newRequest(nethttp.MethodHead, false)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req)
}

// newRequest creates an HTTP request with configured headers and optional conditional headers.
func (s *Source) newRequest(method string, withConditions bool) (*nethttp.Request, error) {
	req, err := nethttp.NewRequestWithContext(context.Background(), method, s.url, nethttp.NoBody)
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
	if method == nethttp.MethodGet && withConditions && s.useConditionalHeaders {
		if s.etag != "" && req.Header.Get("If-Match") == "" {
			req.Header.Set("If-Match", s.etag)
		}
		if s.lastModified != "" && req.Header.Get("If-Unmodified-Since") == "" {
			req.Header.Set("If-Unmodified-Since", s.lastModified)
		}
	}
	return req, nil
}

// rangeRequest performs a GET request for the specified byte range.
func (s *Source) rangeRequest(off, end int64, withConditions bool) (*nethttp.Response, error) {
	req, err := s.newRequest(nethttp.MethodGet, withConditions)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))
	return s.client.Do(req)
}

// hasConditionalHeaders reports whether conditional headers are enabled and available.
func (s *Source) hasConditionalHeaders() bool {
	if !s.useConditionalHeaders {
		return false
	}
	return s.etag != "" || s.lastModified != ""
}

// rangeReadCloser wraps an HTTP response body with a limit reader.
// It drains the body on close to enable connection reuse.
type rangeReadCloser struct {
	body   io.ReadCloser
	reader io.Reader
}

// Read reads from the underlying limit reader.
func (r *rangeReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

// Close drains and closes the underlying response body.
func (r *rangeReadCloser) Close() error {
	_, _ = io.Copy(io.Discard, r.body) //nolint:errcheck // best-effort drain for connection reuse
	return r.body.Close()
}

// parseContentRange extracts the total size from a Content-Range header value.
// It expects the format "bytes start-end/size" and returns the size portion.
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
