package blob

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"time"

	"github.com/meigma/blob/core/internal/blobtype"
	"github.com/meigma/blob/core/internal/file"
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

// newCachedFile creates a cachedFile that wraps f with hash verification.
func newCachedFile(f fs.File, entry *blobtype.Entry, verifyOnClose bool, deleteFunc func([]byte) error) *cachedFile {
	return &cachedFile{
		file:          f,
		entry:         entry,
		verifyOnClose: verifyOnClose,
		deleteFunc:    deleteFunc,
		hasher:        sha256.New(),
	}
}

// Read implements io.Reader, computing a running hash for verification.
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

// ReadAt implements io.ReaderAt for random access without hash verification.
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

// Stat returns file info from the archive entry metadata.
func (f *cachedFile) Stat() (fs.FileInfo, error) {
	return file.NewInfo(f.entry, file.Base(f.entry.Path))
}

// Close closes the underlying file, optionally draining to verify the hash.
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

// verifyHash checks if the accumulated hash matches the expected entry hash.
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

// Stat returns synthetic file info with the cached size.
func (f *bytesFile) Stat() (fs.FileInfo, error) {
	return &bytesFileInfo{size: f.size}, nil
}

// Close is a no-op since the underlying bytes.Reader needs no cleanup.
func (f *bytesFile) Close() error { return nil }

// bytesFileInfo implements fs.FileInfo for bytesFile.
type bytesFileInfo struct {
	size int64
}

// Name returns an empty string since cached content has no file name.
func (fi *bytesFileInfo) Name() string { return "" }

// Size returns the content size in bytes.
func (fi *bytesFileInfo) Size() int64 { return fi.size }

// Mode returns a default file mode.
func (fi *bytesFileInfo) Mode() fs.FileMode { return 0o644 }

// ModTime returns the zero time since cached content has no modification time.
func (fi *bytesFileInfo) ModTime() time.Time { return time.Time{} }

// IsDir returns false since cached content is never a directory.
func (fi *bytesFileInfo) IsDir() bool { return false }

// Sys returns nil since there is no underlying system data.
func (fi *bytesFileInfo) Sys() any { return nil }

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
