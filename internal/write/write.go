package write

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/file"
)

// File streams a file through the hash and optional compression pipeline.
// Returns (dataSize, originalSize, hash, error).
//
// The encoder and buf are reused across calls for performance. Pass nil encoder
// for uncompressed writes. The buf should be at least 32KB for efficient copying.
func File(ctx context.Context, f *os.File, w io.Writer, enc *zstd.Encoder, buf []byte, compression blobtype.Compression, expectedSize int64) (dataSize, originalSize uint64, hash []byte, err error) {
	if expectedSize < 0 {
		return 0, 0, nil, errors.New("negative file size")
	}

	hasher := sha256.New()
	cw := &file.CountingWriter{W: w}
	cr := &file.CountingReader{R: io.LimitReader(f, expectedSize)}

	if compression == blobtype.CompressionNone {
		// Stream: file → TeeReader(hasher) → countingWriter(data)
		if _, err := file.CopyWithContext(ctx, cw, io.TeeReader(cr, hasher), buf); err != nil {
			return 0, 0, nil, wrapOverflowErr(err)
		}
	} else {
		// Stream: file → TeeReader(hasher) → zstd encoder → countingWriter(data)
		enc.Reset(cw)
		if _, err := file.CopyWithContext(ctx, enc, io.TeeReader(cr, hasher), buf); err != nil {
			enc.Close()
			return 0, 0, nil, wrapOverflowErr(err)
		}
		if err := enc.Close(); err != nil {
			return 0, 0, nil, fmt.Errorf("close zstd encoder: %w", err)
		}
	}

	if cr.N != uint64(expectedSize) {
		return 0, 0, nil, fmt.Errorf("file size changed during archive creation: expected %d, got %d", expectedSize, cr.N)
	}

	return cw.N, cr.N, hasher.Sum(nil), nil
}

func wrapOverflowErr(err error) error {
	if errors.Is(err, file.ErrOverflow) {
		return blobtype.ErrSizeOverflow
	}
	return err
}
