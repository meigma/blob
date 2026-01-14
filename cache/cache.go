// Package cache provides content-addressed caching for blob archives.
//
// This package is designed as an optional enhancement to the core blob library,
// adding caching capabilities for improved performance when reading files from
// remote archives.
//
// The cache uses SHA256 hashes of uncompressed file content as keys, providing
// automatic deduplication across archives and implicit integrity verification
// on cache hits.
package cache

import "io"

// Cache provides content-addressed storage for file contents.
//
// Keys are SHA256 hashes of uncompressed file content. Values are the
// uncompressed content. Because keys are content hashes, cache hits
// are implicitly verified—no additional integrity check is needed.
//
// Implementations should handle their own size limits and eviction policies.
type Cache interface {
	// Get retrieves content by its SHA256 hash.
	// Returns nil, false if the content is not cached.
	Get(hash []byte) ([]byte, bool)

	// Put stores content indexed by its SHA256 hash.
	Put(hash []byte, content []byte) error

	// Implementations must be safe for concurrent use.
}

// StreamingCache extends Cache with streaming write support for large files.
//
// Implementations that support streaming (e.g., disk-based caches) should
// implement this interface to allow caching during Open() without buffering
// entire files in memory.
type StreamingCache interface {
	Cache

	// Writer returns a Writer for streaming content into the cache.
	// The hash is the expected SHA256 of the content being written.
	Writer(hash []byte) (Writer, error)
}

// Writer streams content into the cache.
//
// Content is written via Write calls. After all content is written:
//   - Call Commit if the content hash was verified successfully
//   - Call Discard if verification failed or an error occurred
//
// Implementations should buffer writes to a temporary location and only
// make the content available via Cache.Get after Commit is called.
type Writer interface {
	io.Writer

	// Commit finalizes the cache entry, making it available via Get.
	// Must be called after successful hash verification.
	Commit() error

	// Discard aborts the cache write and cleans up temporary data.
	// Must be called if verification fails or an error occurs.
	Discard() error
}
