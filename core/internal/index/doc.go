// Package index provides FlatBuffers index loading and lookup for blob archives.
//
// The index stores file metadata (paths, offsets, sizes, hashes) in a sorted
// structure enabling O(log n) lookups and efficient prefix scanning for
// directory operations.
package index
