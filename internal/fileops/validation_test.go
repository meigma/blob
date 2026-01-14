package fileops

import (
	"crypto/sha256"
	"errors"
	"math"
	"testing"
)

func validEntry() *Entry {
	return &Entry{
		Path:         "test.txt",
		DataOffset:   0,
		DataSize:     100,
		OriginalSize: 100,
		Hash:         make([]byte, sha256.Size),
		Compression:  CompressionNone,
	}
}

func TestValidateForRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		entry       *Entry
		sourceSize  int64
		maxFileSize uint64
		wantErr     error
	}{
		{
			name:        "valid entry within bounds",
			entry:       validEntry(),
			sourceSize:  1000,
			maxFileSize: 0,
			wantErr:     nil,
		},
		{
			name:        "valid entry at exact bounds",
			entry:       &Entry{DataOffset: 0, DataSize: 100, OriginalSize: 100},
			sourceSize:  100,
			maxFileSize: 0,
			wantErr:     nil,
		},
		{
			name:        "negative source size",
			entry:       validEntry(),
			sourceSize:  -1,
			maxFileSize: 0,
			wantErr:     ErrSizeOverflow,
		},
		{
			name:        "data size exceeds max",
			entry:       &Entry{DataOffset: 0, DataSize: 200, OriginalSize: 100},
			sourceSize:  1000,
			maxFileSize: 100,
			wantErr:     ErrSizeOverflow,
		},
		{
			name:        "original size exceeds max",
			entry:       &Entry{DataOffset: 0, DataSize: 50, OriginalSize: 200},
			sourceSize:  1000,
			maxFileSize: 100,
			wantErr:     ErrSizeOverflow,
		},
		{
			name:        "offset + size overflows uint64",
			entry:       &Entry{DataOffset: math.MaxUint64, DataSize: 1, OriginalSize: 1},
			sourceSize:  1000,
			maxFileSize: 0,
			wantErr:     ErrSizeOverflow,
		},
		{
			name:        "data extends beyond source",
			entry:       &Entry{DataOffset: 50, DataSize: 100, OriginalSize: 100},
			sourceSize:  100,
			maxFileSize: 0,
			wantErr:     ErrSizeOverflow,
		},
		{
			name:        "max file size zero disables limit",
			entry:       &Entry{DataOffset: 0, DataSize: 1000000, OriginalSize: 1000000},
			sourceSize:  2000000,
			maxFileSize: 0,
			wantErr:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateForRead(tt.entry, tt.sourceSize, tt.maxFileSize)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateForRead() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hash    []byte
		wantErr bool
	}{
		{
			name:    "valid sha256 hash",
			hash:    make([]byte, sha256.Size),
			wantErr: false,
		},
		{
			name:    "empty hash",
			hash:    nil,
			wantErr: true,
		},
		{
			name:    "short hash",
			hash:    make([]byte, sha256.Size-1),
			wantErr: true,
		},
		{
			name:    "long hash",
			hash:    make([]byte, sha256.Size+1),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := &Entry{Hash: tt.hash}
			err := ValidateHash(entry)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHash() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCompression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		compression  Compression
		dataSize     uint64
		originalSize uint64
		wantErr      error
	}{
		{
			name:         "uncompressed sizes match",
			compression:  CompressionNone,
			dataSize:     100,
			originalSize: 100,
			wantErr:      nil,
		},
		{
			name:         "uncompressed sizes mismatch",
			compression:  CompressionNone,
			dataSize:     50,
			originalSize: 100,
			wantErr:      ErrDecompression,
		},
		{
			name:         "compressed smaller than original",
			compression:  CompressionZstd,
			dataSize:     50,
			originalSize: 100,
			wantErr:      nil,
		},
		{
			name:         "compressed larger than original allowed",
			compression:  CompressionZstd,
			dataSize:     150,
			originalSize: 100,
			wantErr:      nil,
		},
		{
			name:         "compressed sizes equal",
			compression:  CompressionZstd,
			dataSize:     100,
			originalSize: 100,
			wantErr:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := &Entry{
				Compression:  tt.compression,
				DataSize:     tt.dataSize,
				OriginalSize: tt.originalSize,
			}
			err := ValidateCompression(entry)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateCompression() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAll(t *testing.T) {
	t.Parallel()

	t.Run("all validations pass", func(t *testing.T) {
		t.Parallel()
		entry := validEntry()
		err := ValidateAll(entry, 1000, 0)
		if err != nil {
			t.Errorf("ValidateAll() unexpected error: %v", err)
		}
	})

	t.Run("fails on bounds check", func(t *testing.T) {
		t.Parallel()
		entry := validEntry()
		err := ValidateAll(entry, 50, 0) // source too small
		if !errors.Is(err, ErrSizeOverflow) {
			t.Errorf("ValidateAll() error = %v, want ErrSizeOverflow", err)
		}
	})

	t.Run("fails on hash check", func(t *testing.T) {
		t.Parallel()
		entry := validEntry()
		entry.Hash = nil
		err := ValidateAll(entry, 1000, 0)
		if err == nil {
			t.Error("ValidateAll() expected error for invalid hash")
		}
	})

	t.Run("fails on compression check", func(t *testing.T) {
		t.Parallel()
		entry := validEntry()
		entry.DataSize = 50 // mismatch with OriginalSize=100 for CompressionNone
		err := ValidateAll(entry, 1000, 0)
		if !errors.Is(err, ErrDecompression) {
			t.Errorf("ValidateAll() error = %v, want ErrDecompression", err)
		}
	})
}
