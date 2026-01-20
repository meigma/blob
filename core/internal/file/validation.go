package file

import (
	"crypto/sha256"
	"fmt"

	"github.com/meigma/blob/core/internal/sizing"
)

// ValidateForRead checks that an entry is safe to read from a source of the given size.
// It validates:
//   - Source size is non-negative
//   - File sizes are within maxFileSize limit (if limit > 0)
//   - Data offset + size doesn't overflow
//   - Data range is within source bounds
func ValidateForRead(entry *Entry, sourceSize int64, maxFileSize uint64) error {
	if sourceSize < 0 {
		return ErrSizeOverflow
	}

	if maxFileSize > 0 {
		if entry.DataSize > maxFileSize || entry.OriginalSize > maxFileSize {
			return ErrSizeOverflow
		}
	}

	end, ok := sizing.AddUint64(entry.DataOffset, entry.DataSize)
	if !ok {
		return ErrSizeOverflow
	}
	if end > uint64(sourceSize) {
		return ErrSizeOverflow
	}

	return nil
}

// ValidateHash checks that the entry has a valid SHA256 hash.
func ValidateHash(entry *Entry) error {
	if len(entry.Hash) != sha256.Size {
		return fmt.Errorf("invalid hash length: %d", len(entry.Hash))
	}
	return nil
}

// ValidateCompression checks that compression metadata is consistent.
// For uncompressed files, DataSize must equal OriginalSize.
func ValidateCompression(entry *Entry) error {
	if entry.Compression == CompressionNone && entry.DataSize != entry.OriginalSize {
		return fmt.Errorf("%w: size mismatch", ErrDecompression)
	}
	return nil
}

// ValidateAll performs all validation checks for reading an entry.
// This is a convenience function that calls ValidateForRead, ValidateHash,
// and ValidateCompression.
func ValidateAll(entry *Entry, sourceSize int64, maxFileSize uint64) error {
	if err := ValidateForRead(entry, sourceSize, maxFileSize); err != nil {
		return err
	}
	if err := ValidateHash(entry); err != nil {
		return err
	}
	return ValidateCompression(entry)
}
