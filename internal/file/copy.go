package file

import (
	"context"
	"io"
)

// CopyWithContext copies from src to dst until EOF or error, checking for
// context cancellation between reads. It returns the number of bytes written.
//
//nolint:gocognit,unparam // Follows stdlib io.Copy pattern; complexity is inherent to correct I/O handling
func CopyWithContext(ctx context.Context, dst io.Writer, src io.Reader, buf []byte) (uint64, error) {
	var written uint64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				//nolint:gosec // nw is guaranteed non-negative by io.Writer contract
				if written > ^uint64(0)-uint64(nw) {
					return written, ErrOverflow
				}
				written += uint64(nw) //nolint:gosec // overflow checked above
			}
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}
