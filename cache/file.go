package cache

import (
	"bytes"
	"io"
	"io/fs"

	"github.com/meigma/blob/internal/file"
)

// cachedContentFile wraps already-cached content as an fs.File.
type cachedContentFile struct {
	entry   file.Entry
	content []byte
	offset  int
}

func (f *cachedContentFile) Read(p []byte) (int, error) {
	if f.offset >= len(f.content) {
		return 0, io.EOF
	}
	n := copy(p, f.content[f.offset:])
	f.offset += n
	return n, nil
}

func (f *cachedContentFile) Stat() (fs.FileInfo, error) {
	return file.NewInfo(&f.entry, file.Base(f.entry.Path))
}

func (f *cachedContentFile) Close() error {
	return nil
}

// streamingCachedFile wraps a file and streams reads to a Writer.
type streamingCachedFile struct {
	*file.File
	entry   file.Entry
	writer  Writer
	written bool
	failed  bool
}

func (f *streamingCachedFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)

	if n > 0 && !f.failed {
		if _, werr := f.writer.Write(p[:n]); werr != nil {
			// Cache write failed, mark as failed but continue reading
			f.failed = true
		}
		f.written = true
	}

	return n, err
}

func (f *streamingCachedFile) Close() error {
	err := f.File.Close()

	// Handle cache finalization
	switch {
	case f.failed || err != nil:
		_ = f.writer.Discard() //nolint:errcheck // discard is best-effort
	case f.written || f.entry.OriginalSize == 0:
		// Commit on success (or for empty files)
		_ = f.writer.Commit() //nolint:errcheck // caching is opportunistic
	default:
		// Never read anything, discard
		_ = f.writer.Discard() //nolint:errcheck // discard is best-effort
	}

	return err
}

// bufferedCachedFile wraps a file and buffers reads for caching.
type bufferedCachedFile struct {
	*file.File
	entry file.Entry
	cache Cache
	buf   *bytes.Buffer
}

func (f *bufferedCachedFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)

	if n > 0 && f.buf != nil {
		if _, werr := f.buf.Write(p[:n]); werr != nil {
			// Buffer write failed, disable caching
			f.buf = nil
		}
	}

	return n, err
}

func (f *bufferedCachedFile) Close() error {
	err := f.File.Close()

	// Cache content on success
	if err == nil && f.buf != nil && uint64(f.buf.Len()) == f.entry.OriginalSize { //nolint:gosec // Len() is always non-negative
		_ = f.cache.Put(f.entry.Hash, f.buf.Bytes()) //nolint:errcheck // caching is opportunistic
	}

	return err
}
