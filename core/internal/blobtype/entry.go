package blobtype

import (
	"io/fs"
	"time"
)

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
