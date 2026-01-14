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

import (
	"errors"

	"github.com/meigma/blob/internal/blobtype"
)

// Re-export types from internal/blobtype for public API.
type (
	// Entry represents a file in the archive.
	Entry = blobtype.Entry

	// Compression identifies the compression algorithm used for a file.
	Compression = blobtype.Compression
)

// Re-export compression constants.
const (
	CompressionNone = blobtype.CompressionNone
	CompressionZstd = blobtype.CompressionZstd
)

// Sentinel errors re-exported from internal/blobtype.
var (
	// ErrHashMismatch is returned when file content does not match its hash.
	ErrHashMismatch = blobtype.ErrHashMismatch

	// ErrDecompression is returned when decompression fails.
	ErrDecompression = blobtype.ErrDecompression

	// ErrSizeOverflow is returned when byte counts exceed supported limits.
	ErrSizeOverflow = blobtype.ErrSizeOverflow
)

// Sentinel errors specific to the blob package.
var (
	// ErrSymlink is returned when a symlink is encountered where not allowed.
	ErrSymlink = errors.New("blob: symlink")

	// ErrTooManyFiles is returned when the file count exceeds the configured limit.
	ErrTooManyFiles = errors.New("blob: too many files")
)
