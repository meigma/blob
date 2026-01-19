package file

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"time"

	"github.com/meigma/blob/internal/sizing"
)

// File implements fs.File for streaming reads with hash verification.
type File struct {
	reader        *Reader
	entry         Entry
	verifyOnClose bool

	r         io.Reader
	release   func()
	hasher    hash.Hash
	remaining uint64

	initialized bool
	initErr     error
	verified    bool
	verifyErr   error
}

// Interface compliance.
var _ fs.File = (*File)(nil)

// OpenFile creates a new File for streaming reads.
func (r *Reader) OpenFile(entry *Entry, verifyOnClose bool) *File {
	return &File{
		reader:        r,
		entry:         *entry,
		verifyOnClose: verifyOnClose,
	}
}

// Read implements io.Reader with incremental hash verification.
func (f *File) Read(p []byte) (int, error) {
	if err := f.init(); err != nil {
		return 0, err
	}
	if f.verifyErr != nil {
		return 0, f.verifyErr
	}

	if len(p) == 0 {
		return 0, nil
	}

	if f.remaining == 0 {
		return f.readExtra()
	}

	if uint64(len(p)) > f.remaining {
		p = p[:f.remaining]
	}

	n, err := f.r.Read(p)
	if n > 0 {
		_, _ = f.hasher.Write(p[:n]) //nolint:errcheck // hash writes never fail
		f.remaining -= uint64(n)
	}

	if err == io.EOF {
		if f.remaining != 0 {
			return n, fmt.Errorf("%w: unexpected EOF", ErrDecompression)
		}
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

// ReadAt implements io.ReaderAt for uncompressed entries.
// For compressed entries, ReadAt returns an error.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if err := f.init(); err != nil {
		return 0, err
	}
	if f.verifyErr != nil {
		return 0, f.verifyErr
	}
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("read at %d: negative offset", off)
	}
	if f.entry.Compression != CompressionNone {
		return 0, fmt.Errorf("read at %s: unsupported compression", f.entry.Path)
	}

	size, err := sizing.ToInt64(f.entry.OriginalSize, ErrSizeOverflow)
	if err != nil {
		return 0, err
	}
	if off >= size {
		return 0, io.EOF
	}

	expected := len(p)
	if remaining := size - off; remaining < int64(expected) {
		expected = int(remaining)
	}

	dataOffset, err := sizing.ToInt64(f.entry.DataOffset, ErrSizeOverflow)
	if err != nil {
		return 0, err
	}

	n, err := f.reader.source.ReadAt(p[:expected], dataOffset+off)
	if err == io.EOF && n == expected {
		err = nil
	}
	if err != nil {
		return n, err
	}
	if expected < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Stat returns file info.
func (f *File) Stat() (fs.FileInfo, error) {
	return NewInfo(&f.entry, Base(f.entry.Path))
}

// Close releases resources and optionally verifies the hash.
func (f *File) Close() error {
	if err := f.init(); err != nil {
		return err
	}

	defer func() {
		if f.release != nil {
			f.release()
			f.release = nil
		}
	}()

	if f.verified {
		return f.verifyErr
	}
	if !f.verifyOnClose {
		return nil
	}

	// Drain remaining data to verify hash
	buf := make([]byte, 32*1024)
	for {
		_, err := f.Read(buf)
		if err == io.EOF {
			return f.verifyErr
		}
		if err != nil {
			return err
		}
	}
}

func (f *File) init() error {
	if f.initialized {
		return f.initErr
	}
	f.initialized = true

	if err := ValidateForRead(&f.entry, f.reader.source.Size(), f.reader.maxFileSize); err != nil {
		f.initErr = fmt.Errorf("read %s: %w", f.entry.Path, err)
		return f.initErr
	}
	if err := ValidateHash(&f.entry); err != nil {
		f.initErr = fmt.Errorf("read %s: %w", f.entry.Path, err)
		return f.initErr
	}
	if err := ValidateCompression(&f.entry); err != nil {
		f.initErr = err
		return f.initErr
	}

	section, err := f.reader.sectionReader(&f.entry)
	if err != nil {
		f.initErr = err
		return f.initErr
	}

	rd, release, err := f.reader.entryReader(&f.entry, section)
	if err != nil {
		f.initErr = err
		return f.initErr
	}
	f.r = rd
	f.release = release

	f.remaining = f.entry.OriginalSize
	f.hasher = sha256.New()

	return nil
}

func (f *File) readExtra() (int, error) {
	var scratch [1]byte
	n, err := f.r.Read(scratch[:])
	if n > 0 {
		return 0, ErrSizeOverflow
	}
	if err == io.EOF {
		if verifyErr := f.verifyHash(); verifyErr != nil {
			return 0, verifyErr
		}
		return 0, io.EOF
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

func (f *File) verifyHash() error {
	if f.verified {
		return f.verifyErr
	}
	sum := f.hasher.Sum(nil)
	if !bytes.Equal(sum, f.entry.Hash) {
		f.verifyErr = ErrHashMismatch
	}
	f.verified = true
	return f.verifyErr
}

// Info implements fs.FileInfo for regular files.
type Info struct {
	entry Entry
	name  string
	size  int64
}

// NewInfo creates an Info from an entry.
func NewInfo(entry *Entry, name string) (*Info, error) {
	size, err := sizing.ToInt64(entry.OriginalSize, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	return &Info{entry: *entry, name: name, size: size}, nil
}

func (fi *Info) Name() string       { return fi.name }
func (fi *Info) Size() int64        { return fi.size }
func (fi *Info) Mode() fs.FileMode  { return fi.entry.Mode }
func (fi *Info) ModTime() time.Time { return fi.entry.ModTime }
func (fi *Info) IsDir() bool        { return false }
func (fi *Info) Sys() any           { return nil }

// Entry returns the underlying blob entry.
func (fi *Info) Entry() *Entry {
	return &fi.entry
}

// DirInfo implements fs.FileInfo for synthetic directories.
type DirInfo struct {
	name string
}

// NewDirInfo creates a DirInfo with the given name.
func NewDirInfo(name string) *DirInfo {
	return &DirInfo{name: name}
}

func (di *DirInfo) Name() string       { return di.name }
func (di *DirInfo) Size() int64        { return 0 }
func (di *DirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (di *DirInfo) ModTime() time.Time { return time.Time{} }
func (di *DirInfo) IsDir() bool        { return true }
func (di *DirInfo) Sys() any           { return nil }

// DirEntry implements fs.DirEntry by wrapping fs.FileInfo.
type DirEntry struct {
	info    fs.FileInfo
	infoErr error
}

// NewDirEntry creates a DirEntry wrapping the given FileInfo.
func NewDirEntry(info fs.FileInfo, err error) *DirEntry {
	return &DirEntry{info: info, infoErr: err}
}

func (de *DirEntry) Name() string               { return de.info.Name() }
func (de *DirEntry) IsDir() bool                { return de.info.IsDir() }
func (de *DirEntry) Type() fs.FileMode          { return de.info.Mode().Type() }
func (de *DirEntry) Info() (fs.FileInfo, error) { return de.info, de.infoErr }
