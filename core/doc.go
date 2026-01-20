//go:generate flatc --go --go-namespace fb -o internal schema/index.fbs

// Package blob provides a file archive format optimized for random access
// via HTTP range requests against OCI registries.
//
// Archives consist of two OCI blobs:
//   - Index blob: FlatBuffers-encoded file metadata enabling O(log n) lookups
//   - Data blob: Concatenated file contents, sorted by path for efficient directory fetches
//
// The package implements fs.FS and related interfaces for stdlib compatibility.
package blob
