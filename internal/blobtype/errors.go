package blobtype

import "errors"

// Sentinel errors for blob operations.
var (
	// ErrHashMismatch is returned when file content does not match its hash.
	ErrHashMismatch = errors.New("blob: hash verification failed")

	// ErrDecompression is returned when decompression fails.
	ErrDecompression = errors.New("blob: decompression failed")

	// ErrSizeOverflow is returned when byte counts exceed supported limits.
	ErrSizeOverflow = errors.New("blob: size overflow")
)
