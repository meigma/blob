package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/meigma/blob"
	blobhttp "github.com/meigma/blob/http"
)

func newHTTPSource(cfg config, data []byte) (blob.ByteSource, func(), error) {
	if cfg.dataURL == "" {
		return nil, nil, errors.New("data-url is required for HTTP source")
	}

	client := newHTTPClient(cfg)
	url := cfg.dataURL
	var cleanup func()
	if cfg.dataURL == "local" {
		server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			nethttp.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
		}))
		url = server.URL
		cleanup = server.Close
	}

	source, err := blobhttp.NewSource(url, blobhttp.WithClient(client))
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, nil, err
	}
	return source, cleanup, nil
}

func newHTTPClient(cfg config) *nethttp.Client {
	transport := nethttp.DefaultTransport
	if base, ok := transport.(*nethttp.Transport); ok {
		transport = base.Clone()
	}
	if cfg.dataHTTPLatency > 0 || cfg.dataHTTPBPS > 0 {
		transport = &httpThrottleRoundTripper{
			base:           transport,
			latency:        cfg.dataHTTPLatency,
			bytesPerSecond: cfg.dataHTTPBPS,
		}
	}
	return &nethttp.Client{Transport: transport}
}

type httpThrottleRoundTripper struct {
	base           nethttp.RoundTripper
	latency        time.Duration
	bytesPerSecond int64
}

func (rt *httpThrottleRoundTripper) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	if rt.latency > 0 {
		time.Sleep(rt.latency)
	}
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if rt.bytesPerSecond > 0 && resp.Body != nil {
		resp.Body = &throttleReadCloser{
			rc:             resp.Body,
			bytesPerSecond: rt.bytesPerSecond,
			start:          time.Now(),
		}
	}
	return resp, nil
}

type throttleReadCloser struct {
	rc             io.ReadCloser
	bytesPerSecond int64
	start          time.Time
	readBytes      int64
}

func (tr *throttleReadCloser) Read(p []byte) (int, error) {
	n, err := tr.rc.Read(p)
	if n > 0 {
		tr.readBytes += int64(n)
		expected := time.Duration(float64(tr.readBytes) / float64(tr.bytesPerSecond) * float64(time.Second))
		elapsed := time.Since(tr.start)
		if expected > elapsed {
			time.Sleep(expected - elapsed)
		}
	}
	return n, err
}

func (tr *throttleReadCloser) Close() error {
	return tr.rc.Close()
}

func parseBytesPerSecond(value string) (int64, error) {
	text := strings.TrimSpace(value)
	text = strings.TrimSuffix(text, "Bps")
	text = strings.TrimSuffix(text, "bps")
	text = strings.TrimSuffix(text, "/s")
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}

	lower := strings.ToLower(text)
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(lower, "kb"):
		multiplier = 1024
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "k"):
		multiplier = 1024
		text = text[:len(text)-1]
	case strings.HasSuffix(lower, "mb"):
		multiplier = 1024 * 1024
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "m"):
		multiplier = 1024 * 1024
		text = text[:len(text)-1]
	case strings.HasSuffix(lower, "gb"):
		multiplier = 1024 * 1024 * 1024
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "g"):
		multiplier = 1024 * 1024 * 1024
		text = text[:len(text)-1]
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}
	raw, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}
	if raw <= 0 {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}
	return raw * multiplier, nil
}
