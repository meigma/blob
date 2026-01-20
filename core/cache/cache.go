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

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes cached entries until the cache is at or below targetBytes.
	// Returns the number of bytes freed.
	Prune(targetBytes int64) (int64, error)
}
