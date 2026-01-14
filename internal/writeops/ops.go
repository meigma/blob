package writeops

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/ioutil"
)

// Ops handles writing file content with hashing and optional compression.
type Ops struct {
	encoder *zstd.Encoder
	buf     []byte
}

// Option configures an Ops instance.
type Option func(*Ops)

// New creates a new Ops instance for writing files.
// If compression is enabled, pass a non-nil encoder.
func New(enc *zstd.Encoder) *Ops {
	return &Ops{
		encoder: enc,
		buf:     make([]byte, 32*1024),
	}
}

// WriteFile streams a file through the hash and optional compression pipeline.
// Returns (dataSize, originalSize, hash, error).
func (o *Ops) WriteFile(ctx context.Context, f *os.File, w io.Writer, compression blobtype.Compression, expectedSize int64) (dataSize, originalSize uint64, hash []byte, err error) {
	if expectedSize < 0 {
		return 0, 0, nil, errors.New("negative file size")
	}

	hasher := sha256.New()
	cw := &ioutil.CountingWriter{W: w}
	cr := &ioutil.CountingReader{R: io.LimitReader(f, expectedSize)}

	if compression == blobtype.CompressionNone {
		// Stream: file → TeeReader(hasher) → countingWriter(data)
		if _, err := ioutil.CopyWithContext(ctx, cw, io.TeeReader(cr, hasher), o.buf); err != nil {
			return 0, 0, nil, wrapOverflowErr(err)
		}
	} else {
		// Stream: file → TeeReader(hasher) → zstd encoder → countingWriter(data)
		o.encoder.Reset(cw)
		if _, err := ioutil.CopyWithContext(ctx, o.encoder, io.TeeReader(cr, hasher), o.buf); err != nil {
			o.encoder.Close()
			return 0, 0, nil, wrapOverflowErr(err)
		}
		if err := o.encoder.Close(); err != nil {
			return 0, 0, nil, fmt.Errorf("close zstd encoder: %w", err)
		}
	}

	if cr.N != uint64(expectedSize) {
		return 0, 0, nil, fmt.Errorf("file size changed during archive creation: expected %d, got %d", expectedSize, cr.N)
	}

	return cw.N, cr.N, hasher.Sum(nil), nil
}

func wrapOverflowErr(err error) error {
	if errors.Is(err, ioutil.ErrOverflow) {
		return blobtype.ErrSizeOverflow
	}
	return err
}
