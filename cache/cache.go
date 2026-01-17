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

import "io/fs"

// Cache provides content-addressed file storage.
//
// Keys are SHA256 hashes of uncompressed file content. Because keys are
// content hashes, cache hits are implicitly verified—no additional integrity
// check is needed.
//
// Implementations should handle their own size limits and eviction policies.
// Implementations must be safe for concurrent use.
type Cache interface {
	// Get returns an fs.File for reading cached content.
	// Returns nil, false if content is not cached.
	// Each call returns a new file handle (safe for concurrent use).
	Get(hash []byte) (fs.File, bool)

	// Put stores content by reading from the provided fs.File.
	// The cache reads the file to completion; caller still owns/closes the file.
	Put(hash []byte, f fs.File) error

	// Delete removes cached content for the given hash.
	// Implementations should treat missing entries as a no-op.
	Delete(hash []byte) error
}
