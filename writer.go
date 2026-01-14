package blob

import (
	"context"
	"io"
)

// WriteOptions configures archive creation.
type WriteOptions struct {
	// Compression specifies the algorithm to use for compressing files.
	// Use CompressionNone to store files uncompressed.
	Compression Compression

	// TODO: Per-file compression predicate for selective compression
	// CompressFile func(path string, size int64) bool
}

// Writer creates archives from directories.
type Writer struct {
	opts WriteOptions
}

// NewWriter creates a Writer with the given options.
func NewWriter(opts WriteOptions) *Writer {
	return &Writer{opts: opts}
}

// Create builds an archive from the contents of dir.
//
// Files are written to the data writer in path-sorted order, enabling
// efficient directory fetches via single range requests. The index is
// written as a FlatBuffers-encoded blob to the index writer.
//
// Create walks dir recursively, including all regular files. Empty
// directories are not preserved. Symbolic links are not followed.
//
// The context can be used for cancellation of long-running archive creation.
func (w *Writer) Create(ctx context.Context, dir string, index, data io.Writer) error {
	panic("not implemented")
}
