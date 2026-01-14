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
	"io/fs"
	"time"
)

// Sentinel errors.
var (
	// ErrHashMismatch is returned when file content does not match its hash.
	ErrHashMismatch = errors.New("blob: hash verification failed")

	// ErrDecompression is returned when decompression fails.
	ErrDecompression = errors.New("blob: decompression failed")

	// ErrSizeOverflow is returned when byte counts exceed supported limits.
	ErrSizeOverflow = errors.New("blob: size overflow")

	// ErrSymlink is returned when a symlink is encountered where not allowed.
	ErrSymlink = errors.New("blob: symlink")

	// ErrTooManyFiles is returned when the file count exceeds the configured limit.
	ErrTooManyFiles = errors.New("blob: too many files")
)

// Compression identifies the compression algorithm used for a file.
type Compression uint8

const (
	CompressionNone Compression = iota
	CompressionZstd
)

func (c Compression) String() string {
	switch c {
	case CompressionNone:
		return "none"
	case CompressionZstd:
		return "zstd"
	default:
		return "unknown"
	}
}

// Entry represents a file in the archive.
type Entry struct {
	// Path is the file path relative to the archive root (e.g., "src/main.go").
	Path string

	// DataOffset is the byte offset in the data blob where this file's content begins.
	DataOffset uint64

	// DataSize is the size in bytes of the file's content in the data blob.
	// For compressed files, this is the compressed size.
	DataSize uint64

	// OriginalSize is the uncompressed size in bytes.
	// Equal to DataSize for uncompressed files.
	OriginalSize uint64

	// Hash is the SHA256 hash of the uncompressed file content.
	Hash []byte

	// Mode is the file's permission bits.
	Mode fs.FileMode

	// UID is the file owner's user ID.
	UID uint32

	// GID is the file owner's group ID.
	GID uint32

	// ModTime is the file's modification time.
	ModTime time.Time

	// Compression is the algorithm used to compress this file.
	Compression Compression
}
