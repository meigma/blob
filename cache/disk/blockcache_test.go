package disk

import (
	"io"
	"sync/atomic"
	"testing"

	blobcache "github.com/meigma/blob/cache"
)

type countingSource struct {
	data     []byte
	sourceID string
	reads    atomic.Int64
}

func (s *countingSource) ReadAt(p []byte, off int64) (int, error) {
	s.reads.Add(1)
	if off >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[off:])
	if off+int64(n) >= int64(len(s.data)) {
		return n, io.EOF
	}
	return n, nil
}

func (s *countingSource) Size() int64 {
	return int64(len(s.data))
}

func (s *countingSource) SourceID() string {
	return s.sourceID
}

func (s *countingSource) Reads() int64 {
	return s.reads.Load()
}

func TestBlockCacheReadAtReuse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache, err := NewBlockCache(dir)
	if err != nil {
		t.Fatalf("NewBlockCache() error = %v", err)
	}

	src := &countingSource{
		data:     []byte("abcdefghijklmnopqrstuvwxyz"),
		sourceID: "source:test",
	}
	cached, err := cache.Wrap(src, blobcache.WithBlockSize(8))
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}

	buf := make([]byte, 4)
	n, err := cached.ReadAt(buf, 2)
	if err != nil {
		t.Fatalf("ReadAt() error = %v", err)
	}
	if n != 4 || string(buf) != "cdef" {
		t.Fatalf("ReadAt() got %q (n=%d), want %q", string(buf), n, "cdef")
	}
	if reads := src.Reads(); reads != 1 {
		t.Fatalf("source reads = %d, want 1", reads)
	}

	buf = make([]byte, 3)
	n, err = cached.ReadAt(buf, 5)
	if err != nil {
		t.Fatalf("ReadAt() error = %v", err)
	}
	if n != 3 || string(buf) != "fgh" {
		t.Fatalf("ReadAt() got %q (n=%d), want %q", string(buf), n, "fgh")
	}
	if reads := src.Reads(); reads != 1 {
		t.Fatalf("source reads = %d, want 1 (cache hit)", reads)
	}

	buf = make([]byte, 2)
	n, err = cached.ReadAt(buf, 9)
	if err != nil {
		t.Fatalf("ReadAt() error = %v", err)
	}
	if n != 2 || string(buf) != "jk" {
		t.Fatalf("ReadAt() got %q (n=%d), want %q", string(buf), n, "jk")
	}
	if reads := src.Reads(); reads != 2 {
		t.Fatalf("source reads = %d, want 2", reads)
	}
}

func TestBlockCacheWrapEmptySourceID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache, err := NewBlockCache(dir)
	if err != nil {
		t.Fatalf("NewBlockCache() error = %v", err)
	}

	src := &countingSource{
		data:     []byte("data"),
		sourceID: "",
	}
	if _, err := cache.Wrap(src); err == nil {
		t.Fatal("Wrap() error = nil, want error")
	}
}
