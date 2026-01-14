// Package fileops provides internal file reading operations for the blob package.
package fileops

import "github.com/meigma/blob/internal/blobtype"

// Re-export types from blobtype to avoid import changes throughout fileops.
type (
	Entry       = blobtype.Entry
	Compression = blobtype.Compression
)

// Re-export compression constants.
const (
	CompressionNone = blobtype.CompressionNone
	CompressionZstd = blobtype.CompressionZstd
)

// Re-export sentinel errors.
var (
	ErrHashMismatch  = blobtype.ErrHashMismatch
	ErrDecompression = blobtype.ErrDecompression
	ErrSizeOverflow  = blobtype.ErrSizeOverflow
)
