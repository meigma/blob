package file

import (
	"errors"
	"io"
)

// ErrOverflow indicates a counter exceeded its maximum value.
var ErrOverflow = errors.New("counter overflow")

// CountingReader wraps a reader and counts bytes read.
type CountingReader struct {
	R io.Reader
	N uint64
}

// Read implements io.Reader.
func (cr *CountingReader) Read(p []byte) (int, error) {
	n, err := cr.R.Read(p)
	if n > 0 {
		//nolint:gosec // n is guaranteed non-negative by io.Reader contract
		if cr.N > ^uint64(0)-uint64(n) {
			return n, ErrOverflow
		}
		cr.N += uint64(n) //nolint:gosec // overflow checked above
	}
	return n, err
}

// CountingWriter wraps a writer and counts bytes written.
type CountingWriter struct {
	W io.Writer
	N uint64
}

// Write implements io.Writer.
func (cw *CountingWriter) Write(p []byte) (int, error) {
	n, err := cw.W.Write(p)
	if n > 0 {
		//nolint:gosec // n is guaranteed non-negative by io.Writer contract
		if cw.N > ^uint64(0)-uint64(n) {
			return n, ErrOverflow
		}
		cw.N += uint64(n) //nolint:gosec // overflow checked above
	}
	return n, err
}
