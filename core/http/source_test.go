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

func TestSource_ReadAt(t *testing.T) {
	t.Parallel()

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

	tests := []struct {
		name    string
		bufSize int
		offset  int64
		wantN   int
		wantErr error
		want    string
	}{
		{
			name:    "read from middle",
			bufSize: 5,
			offset:  6,
			wantN:   5,
			wantErr: nil,
			want:    "world",
		},
		{
			name:    "read past end returns EOF",
			bufSize: 10,
			offset:  int64(len(data) - 3),
			wantN:   3,
			wantErr: io.EOF,
			want:    "rld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := make([]byte, tt.bufSize)
			n, err := src.ReadAt(buf, tt.offset)
			if err != tt.wantErr {
				t.Fatalf("ReadAt() error = %v, want %v", err, tt.wantErr)
			}
			if n != tt.wantN {
				t.Fatalf("ReadAt() n = %d, want %d", n, tt.wantN)
			}
			if got := string(buf[:n]); got != tt.want {
				t.Fatalf("ReadAt() got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewSource_RangeUnsupported(t *testing.T) {
	t.Parallel()

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

func TestSource_ReadAt_RetriesWithoutIfMatchOn412(t *testing.T) {
	t.Parallel()

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
