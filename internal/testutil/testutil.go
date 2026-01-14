package testutil

import (
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// MockByteSource implements a simple in-memory byte source for tests.
type MockByteSource struct {
	data []byte
}

// NewMockByteSource returns a byte source backed by the provided data.
func NewMockByteSource(data []byte) *MockByteSource {
	return &MockByteSource{data: data}
}

// ReadAt implements io.ReaderAt semantics over the backing slice.
func (m *MockByteSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}
	return n, nil
}

// Size returns the total size of the backing data.
func (m *MockByteSource) Size() int64 {
	return int64(len(m.data))
}

// Bytes returns the backing slice for tests that need to mutate data.
func (m *MockByteSource) Bytes() []byte {
	return m.data
}

// CacheWriter is the streaming writer interface used by DiskCache.
type CacheWriter interface {
	Write(p []byte) (int, error)
	Commit() error
	Discard() error
}

// MockCache implements a basic concurrency-safe cache for tests.
type MockCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMockCache constructs an empty in-memory cache.
func NewMockCache() *MockCache {
	return &MockCache{data: make(map[string][]byte)}
}

// Get retrieves data by hash.
func (c *MockCache) Get(hash []byte) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.data[string(hash)]
	return data, ok
}

// Put stores data by hash.
func (c *MockCache) Put(hash, content []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[string(hash)] = content
	return nil
}

// DiskCache implements a simple disk-backed cache with streaming support.
type DiskCache struct {
	dir string
}

// NewDiskCache creates a disk-backed cache rooted at dir.
func NewDiskCache(dir string) *DiskCache {
	return &DiskCache{dir: dir}
}

// Get retrieves cached content by hash.
func (c *DiskCache) Get(hash []byte) ([]byte, bool) {
	path := c.path(hash)
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from hash, not user input
	if err != nil {
		return nil, false
	}
	return data, true
}

// Put stores cached content by hash.
func (c *DiskCache) Put(hash, content []byte) error {
	finalPath := c.path(hash)
	if _, err := os.Stat(finalPath); err == nil {
		return nil
	}

	tmp, err := os.CreateTemp(c.dir, "cache-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// Writer opens a streaming cache writer for the given hash.
func (c *DiskCache) Writer(hash []byte) (CacheWriter, error) {
	finalPath := c.path(hash)
	if _, err := os.Stat(finalPath); err == nil {
		return &noopWriter{}, nil
	}

	tmp, err := os.CreateTemp(c.dir, "cache-*")
	if err != nil {
		return nil, err
	}
	return &diskCacheWriter{
		file:      tmp,
		tmpPath:   tmp.Name(),
		finalPath: finalPath,
	}, nil
}

func (c *DiskCache) path(hash []byte) string {
	return filepath.Join(c.dir, hex.EncodeToString(hash))
}

type diskCacheWriter struct {
	file      *os.File
	tmpPath   string
	finalPath string
}

func (w *diskCacheWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

func (w *diskCacheWriter) Commit() error {
	if err := w.file.Close(); err != nil {
		_ = os.Remove(w.tmpPath)
		return err
	}
	if err := os.Rename(w.tmpPath, w.finalPath); err != nil {
		_ = os.Remove(w.tmpPath)
		return err
	}
	return nil
}

func (w *diskCacheWriter) Discard() error {
	if w.file != nil {
		_ = w.file.Close()
	}
	return os.Remove(w.tmpPath)
}

type noopWriter struct{}

func (w *noopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *noopWriter) Commit() error               { return nil }
func (w *noopWriter) Discard() error              { return nil }
