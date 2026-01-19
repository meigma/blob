package http_test

import (
	"bytes"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	blobhttp "github.com/meigma/blob/core/http"
)

func TestSourceReadAt(t *testing.T) {
	data := []byte("hello world")
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		nethttp.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(server.Close)

	src, err := blobhttp.NewSource(server.URL, blobhttp.WithConditionalHeaders())
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	if src.Size() != int64(len(data)) {
		t.Fatalf("Size() = %d, want %d", src.Size(), len(data))
	}

	buf := make([]byte, 5)
	n, err := src.ReadAt(buf, 6)
	if err != nil {
		t.Fatalf("ReadAt() error = %v", err)
	}
	if n != len(buf) {
		t.Fatalf("ReadAt() n = %d, want %d", n, len(buf))
	}
	if string(buf) != "world" {
		t.Fatalf("ReadAt() got %q, want %q", string(buf), "world")
	}

	edge := make([]byte, 10)
	n, err = src.ReadAt(edge, int64(len(data)-3))
	if err != io.EOF {
		t.Fatalf("ReadAt() error = %v, want io.EOF", err)
	}
	if n != 3 {
		t.Fatalf("ReadAt() n = %d, want 3", n)
	}
	if string(edge[:n]) != "rld" {
		t.Fatalf("ReadAt() got %q, want %q", string(edge[:n]), "rld")
	}
}

func TestSourceRangeUnsupported(t *testing.T) {
	data := []byte("range unsupported")
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method == nethttp.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	_, err := blobhttp.NewSource(server.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSourceReadAtRetriesWithoutIfMatchOn412(t *testing.T) {
	data := []byte("hello world")
	etag := `"retry-test"`
	var withIfMatchRange int32
	var withoutIfMatchRange int32

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("ETag", etag)
			return
		case nethttp.MethodGet:
			if r.Header.Get("Range") == "bytes=6-10" {
				if r.Header.Get("If-Match") != "" {
					atomic.AddInt32(&withIfMatchRange, 1)
					w.WriteHeader(nethttp.StatusPreconditionFailed)
					return
				}
				atomic.AddInt32(&withoutIfMatchRange, 1)
			}
			w.Header().Set("ETag", etag)
			nethttp.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
			return
		default:
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)

	src, err := blobhttp.NewSource(server.URL, blobhttp.WithConditionalHeaders())
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}

	buf := make([]byte, 5)
	n, err := src.ReadAt(buf, 6)
	if err != nil {
		t.Fatalf("ReadAt() error = %v", err)
	}
	if got := string(buf[:n]); got != "world" {
		t.Fatalf("ReadAt() got %q, want %q", got, "world")
	}
	if atomic.LoadInt32(&withIfMatchRange) != 1 {
		t.Fatalf("expected one range request with If-Match, got %d", withIfMatchRange)
	}
	if atomic.LoadInt32(&withoutIfMatchRange) != 1 {
		t.Fatalf("expected one range retry without If-Match, got %d", withoutIfMatchRange)
	}
}
