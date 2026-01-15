package testutil

import (
	"io"
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
