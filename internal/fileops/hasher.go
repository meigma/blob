package fileops

import (
	"hash"
	"io"
)

// HashingReader wraps an io.Reader and computes a hash of all data read.
type HashingReader struct {
	r io.Reader
	h hash.Hash
}

// NewHashingReader creates a reader that computes a hash while reading.
func NewHashingReader(r io.Reader, h hash.Hash) *HashingReader {
	return &HashingReader{r: r, h: h}
}

// Read implements io.Reader.
func (hr *HashingReader) Read(p []byte) (int, error) {
	n, err := hr.r.Read(p)
	if n > 0 {
		_, _ = hr.h.Write(p[:n]) //nolint:errcheck // hash writes never fail
	}
	return n, err
}

// Sum returns the hash sum computed so far.
func (hr *HashingReader) Sum() []byte {
	return hr.h.Sum(nil)
}

// EnsureNoExtra reads from r and returns an error if any data is available.
// This is used to detect when decompressed data exceeds the expected size.
func EnsureNoExtra(r io.Reader) error {
	var scratch [1]byte
	n, err := r.Read(scratch[:])
	if n > 0 {
		return ErrSizeOverflow
	}
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}
