package testutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"sync"
	"time"
)

// MockByteSource implements a simple in-memory byte source for tests.
type MockByteSource struct {
	data     []byte
	sourceID string
}

// NewMockByteSource returns a byte source backed by the provided data.
func NewMockByteSource(data []byte) *MockByteSource {
	sum := sha256.Sum256(data)
	return &MockByteSource{
		data:     data,
		sourceID: "mock:" + hex.EncodeToString(sum[:]),
	}
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

// SourceID returns a stable identifier for the source data.
func (m *MockByteSource) SourceID() string {
	return m.sourceID
}

// Bytes returns the backing slice for tests that need to mutate data.
func (m *MockByteSource) Bytes() []byte {
	return m.data
}

// MockCache implements a basic concurrency-safe cache for tests.
type MockCache struct {
	mu   sync.RWMutex
	data map[string][]byte
	max  int64
}

// NewMockCache constructs an empty in-memory cache.
func NewMockCache() *MockCache {
	return &MockCache{data: make(map[string][]byte)}
}

// Get returns an fs.File for reading cached content.
func (c *MockCache) Get(hash []byte) (fs.File, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.data[string(hash)]
	if !ok {
		return nil, false
	}
	return &mockCacheFile{Reader: bytes.NewReader(data), size: int64(len(data))}, true
}

// Put stores content by reading from the provided fs.File.
func (c *MockCache) Put(hash []byte, f fs.File) error {
	content, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[string(hash)] = content
	return nil
}

// Delete removes cached content for the given hash.
func (c *MockCache) Delete(hash []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, string(hash))
	return nil
}

// MaxBytes returns the configured cache size limit (0 = unlimited).
func (c *MockCache) MaxBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.max
}

// SizeBytes returns the current cache size in bytes.
func (c *MockCache) SizeBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var total int64
	for _, data := range c.data {
		total += int64(len(data))
	}
	return total
}

// Prune removes cached entries until the cache is at or below targetBytes.
func (c *MockCache) Prune(targetBytes int64) (int64, error) {
	if targetBytes < 0 {
		targetBytes = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for _, data := range c.data {
		total += int64(len(data))
	}
	if total <= targetBytes {
		return 0, nil
	}
	var freed int64
	for key, data := range c.data {
		if total <= targetBytes {
			break
		}
		delete(c.data, key)
		total -= int64(len(data))
		freed += int64(len(data))
	}
	return freed, nil
}

// GetBytes retrieves raw bytes by hash (for test assertions).
func (c *MockCache) GetBytes(hash []byte) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.data[string(hash)]
	return data, ok
}

// mockCacheFile wraps a bytes.Reader to implement fs.File.
type mockCacheFile struct {
	*bytes.Reader
	size int64
}

func (f *mockCacheFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{size: f.size}, nil
}

func (f *mockCacheFile) Close() error {
	return nil
}

// mockFileInfo implements fs.FileInfo for mockCacheFile.
type mockFileInfo struct {
	size int64
}

func (fi *mockFileInfo) Name() string       { return "" }
func (fi *mockFileInfo) Size() int64        { return fi.size }
func (fi *mockFileInfo) Mode() fs.FileMode  { return 0o644 }
func (fi *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *mockFileInfo) IsDir() bool        { return false }
func (fi *mockFileInfo) Sys() any           { return nil }
