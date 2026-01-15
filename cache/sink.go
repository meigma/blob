package cache

import (
	"bytes"
	"math"

	"github.com/meigma/blob/internal/batch"
)

// cacheSink implements batch.Sink for caching to a Cache.
type cacheSink struct {
	cache Cache
}

type nonStreamingCacheSink struct {
	*cacheSink
}

func (s *nonStreamingCacheSink) PutBuffered(entry *batch.Entry, content []byte) error {
	_ = s.cache.Put(entry.Hash, content) //nolint:errcheck // caching is opportunistic
	return nil
}

// ShouldProcess returns false if the entry is already cached.
func (s *cacheSink) ShouldProcess(entry *batch.Entry) bool {
	_, cached := s.cache.Get(entry.Hash)
	return !cached
}

// Writer returns a Committer that writes to the cache.
func (s *cacheSink) Writer(entry *batch.Entry) (batch.Committer, error) {
	// If StreamingCache, use its Writer directly
	if sc, ok := s.cache.(StreamingCache); ok {
		w, err := sc.Writer(entry.Hash)
		if err == nil {
			return w, nil
		}
		// Fall back to buffered on error (e.g., disk full, permission denied).
		// This allows prefetch to continue even if streaming cache is unavailable.
	}
	// Basic Cache: buffer in memory
	expectedSize := 0
	if entry.OriginalSize > 0 && entry.OriginalSize <= uint64(math.MaxInt) {
		expectedSize = int(entry.OriginalSize)
	}
	return newBufferCommitter(s.cache, entry.Hash, expectedSize), nil
}

// bufferCommitter buffers writes in memory and caches on Commit.
type bufferCommitter struct {
	cache Cache
	hash  []byte
	buf   bytes.Buffer
}

func newBufferCommitter(cache Cache, hash []byte, expectedSize int) *bufferCommitter {
	committer := &bufferCommitter{
		cache: cache,
		hash:  hash,
	}
	if expectedSize > 0 {
		committer.buf.Grow(expectedSize)
	}
	return committer
}

// Write implements io.Writer.
func (c *bufferCommitter) Write(p []byte) (int, error) {
	return c.buf.Write(p)
}

// Commit stores the buffered content in the cache.
func (c *bufferCommitter) Commit() error {
	// Cache errors are non-fatal for prefetch operations
	_ = c.cache.Put(c.hash, c.buf.Bytes()) //nolint:errcheck // caching is opportunistic
	return nil
}

// Discard clears the buffer (no cleanup needed for in-memory buffer).
func (c *bufferCommitter) Discard() error {
	c.buf.Reset()
	return nil
}
