// Package blobtype defines shared types used across the blob package and its
// internal packages. This avoids circular imports between blob and internal/file.
package blobtype

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
