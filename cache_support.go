package blob

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"time"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/file"
)

// cachedFile wraps an fs.File to provide archive entry metadata and hash verification.
type cachedFile struct {
	file          fs.File
	entry         *blobtype.Entry
	verifyOnClose bool
	deleteFunc    func([]byte) error
	hasher        hash.Hash
	verified      bool
	verifyErr     error
}

func newCachedFile(f fs.File, entry *blobtype.Entry, verifyOnClose bool, deleteFunc func([]byte) error) *cachedFile {
	return &cachedFile{
		file:          f,
		entry:         entry,
		verifyOnClose: verifyOnClose,
		deleteFunc:    deleteFunc,
		hasher:        sha256.New(),
	}
}

func (f *cachedFile) Read(p []byte) (int, error) {
	if f.verifyErr != nil {
		return 0, f.verifyErr
	}
	n, err := f.file.Read(p)
	if n > 0 {
		_, _ = f.hasher.Write(p[:n]) //nolint:errcheck // hash writes never fail
	}
	if err == io.EOF {
		if verifyErr := f.verifyHash(); verifyErr != nil {
			return n, verifyErr
		}
		return n, io.EOF
	}
	if err != nil {
		return n, err
	}
	return n, nil
}

func (f *cachedFile) ReadAt(p []byte, off int64) (int, error) {
	if f.verifyErr != nil {
		return 0, f.verifyErr
	}
	readerAt, ok := f.file.(io.ReaderAt)
	if !ok {
		return 0, fmt.Errorf("read at %s: reader does not support random access", f.entry.Path)
	}
	return readerAt.ReadAt(p, off)
}

func (f *cachedFile) Stat() (fs.FileInfo, error) {
	return file.NewInfo(f.entry, file.Base(f.entry.Path))
}

func (f *cachedFile) Close() error {
	if f.verified || !f.verifyOnClose {
		closeErr := f.file.Close()
		if f.verifyErr != nil {
			return f.verifyErr
		}
		return closeErr
	}

	buf := make([]byte, 32*1024)
	for {
		_, err := f.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = f.file.Close()
			return err
		}
	}

	closeErr := f.file.Close()
	if f.verifyErr != nil {
		return f.verifyErr
	}
	return closeErr
}

func (f *cachedFile) verifyHash() error {
	if f.verified {
		return f.verifyErr
	}
	sum := f.hasher.Sum(nil)
	if !bytes.Equal(sum, f.entry.Hash) {
		f.verifyErr = ErrHashMismatch
		if f.deleteFunc != nil {
			_ = f.deleteFunc(f.entry.Hash) //nolint:errcheck // best-effort cache cleanup on hash mismatch
		}
	}
	f.verified = true
	return f.verifyErr
}

// bytesFile wraps []byte as fs.File (for ReadFile's Put after in-memory read).
type bytesFile struct {
	*bytes.Reader
	size int64
}

func (f *bytesFile) Stat() (fs.FileInfo, error) {
	return &bytesFileInfo{size: f.size}, nil
}

func (f *bytesFile) Close() error { return nil }

// bytesFileInfo implements fs.FileInfo for bytesFile.
type bytesFileInfo struct {
	size int64
}

func (fi *bytesFileInfo) Name() string       { return "" }
func (fi *bytesFileInfo) Size() int64        { return fi.size }
func (fi *bytesFileInfo) Mode() fs.FileMode  { return 0o644 }
func (fi *bytesFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *bytesFileInfo) IsDir() bool        { return false }
func (fi *bytesFileInfo) Sys() any           { return nil }

// ensureCached populates the cache for an entry if not already cached.
// Uses singleflight to prevent duplicate fetches.
func (b *Blob) ensureCached(entry *Entry) error {
	_, err, _ := b.cacheGroup.Do(string(entry.Hash), func() (any, error) {
		// Double-check after acquiring singleflight
		if f, ok := b.cache.Get(entry.Hash); ok {
			_ = f.Close()
			return struct{}{}, nil //nolint:nilnil // returning nil error is intentional for cache hit
		}

		// Stream from source to cache
		f := b.reader.OpenFile(entry, true)
		err := b.cache.Put(entry.Hash, f)
		f.Close()
		return struct{}{}, err
	})
	return err
}
