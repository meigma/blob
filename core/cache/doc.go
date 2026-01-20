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
