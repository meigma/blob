package blob

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"iter"
	"math"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// ByteSource provides random access to the data blob.
//
// Implementations exist for local files (*os.File) and HTTP range requests.
type ByteSource interface {
	io.ReaderAt
	Size() int64
}

const defaultMaxFileSize = 256 << 20
const defaultMaxDecoderMemory = 256 << 20

// ReaderOption configures a Reader.
type ReaderOption func(*Reader)

// WithMaxFileSize limits the maximum per-file size (compressed and uncompressed).
// Set limit to 0 to disable the limit.
func WithMaxFileSize(limit uint64) ReaderOption {
	return func(r *Reader) {
		r.maxFileSize = limit
	}
}

// WithMaxDecoderMemory limits the maximum memory used by the zstd decoder.
// Set limit to 0 to disable the limit.
func WithMaxDecoderMemory(limit uint64) ReaderOption {
	return func(r *Reader) {
		r.maxDecoderMemory = limit
	}
}

// WithVerifyOnClose controls whether Close drains the file to verify the hash.
//
// When false, Close returns without reading the remaining data. Integrity is
// only guaranteed when callers read to EOF.
func WithVerifyOnClose(enabled bool) ReaderOption {
	return func(r *Reader) {
		r.verifyOnClose = enabled
	}
}

// Reader provides random access to archive files.
//
// Reader implements fs.FS, fs.StatFS, fs.ReadFileFS, and fs.ReadDirFS
// for compatibility with the standard library.
type Reader struct {
	index            *Index
	source           ByteSource
	maxFileSize      uint64
	maxDecoderMemory uint64
	verifyOnClose    bool
}

// Interface compliance.
var (
	_ fs.FS         = (*Reader)(nil)
	_ fs.StatFS     = (*Reader)(nil)
	_ fs.ReadFileFS = (*Reader)(nil)
	_ fs.ReadDirFS  = (*Reader)(nil)
)

// NewReader creates a Reader for accessing files in the archive.
//
// The index provides file metadata and the source provides access to file content.
// Options can be used to configure size and decoder limits.
func NewReader(index *Index, source ByteSource, opts ...ReaderOption) *Reader {
	r := &Reader{
		index:            index,
		source:           source,
		maxFileSize:      defaultMaxFileSize,
		maxDecoderMemory: defaultMaxDecoderMemory,
		verifyOnClose:    true,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Open implements fs.FS.
//
// Open returns an fs.File for reading the named file. The returned file
// verifies the content hash on Close (unless disabled by WithVerifyOnClose)
// and returns ErrHashMismatch if verification fails. Callers must read to
// EOF or Close to ensure integrity; partial reads may return unverified data.
func (r *Reader) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	if entry, ok := r.index.Lookup(name); ok {
		return &file{r: r, entry: entry}, nil
	}

	// Check if it's a directory
	if r.isDir(name) {
		return &openDir{r: r, name: name}, nil
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// Stat implements fs.StatFS.
//
// Stat returns file info for the named file without reading its content.
// For directories (paths that are prefixes of other entries), Stat returns
// synthetic directory info.
func (r *Reader) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	if entry, ok := r.index.Lookup(name); ok {
		info, err := newFileInfo(entry, pathBase(name))
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
		}
		return info, nil
	}

	// Check if it's a directory
	if r.isDir(name) {
		dirName := pathBase(name)
		if name == "." {
			dirName = "."
		}
		return &dirInfo{name: dirName}, nil
	}

	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// ReadFile implements fs.ReadFileFS.
//
// ReadFile reads and returns the entire contents of the named file.
// The content is decompressed if necessary and verified against its hash.
func (r *Reader) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	entry, ok := r.index.Lookup(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}

	return r.readAndVerify(&entry)
}

// ReadDir implements fs.ReadDirFS.
//
// ReadDir returns directory entries for the named directory, sorted by name.
// Directory entries are synthesized from file paths—the archive does not
// store directories explicitly.
func (r *Reader) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	prefix := dirPrefix(name)
	dirIter := newDirIter(r.index, prefix)
	defer dirIter.Close()

	entries := make([]fs.DirEntry, 0)
	for {
		entry, ok := dirIter.Next()
		if !ok {
			break
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 && name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}

	return entries, nil
}

// rangeGroup represents a contiguous range of entries in the data blob.
type rangeGroup struct {
	start   uint64
	end     uint64
	entries []Entry
}

// groupAdjacentEntries groups entries that are adjacent in the data blob.
func groupAdjacentEntries(entries []Entry) []rangeGroup {
	groups := make([]rangeGroup, 0, len(entries))
	current := rangeGroup{
		start:   entries[0].DataOffset,
		end:     entries[0].DataOffset + entries[0].DataSize,
		entries: []Entry{entries[0]},
	}

	for i := 1; i < len(entries); i++ {
		entry := entries[i]
		entryEnd := entry.DataOffset + entry.DataSize

		if entry.DataOffset == current.end {
			current.end = entryEnd
			current.entries = append(current.entries, entry)
		} else {
			groups = append(groups, current)
			current = rangeGroup{
				start:   entry.DataOffset,
				end:     entryEnd,
				entries: []Entry{entry},
			}
		}
	}
	return append(groups, current)
}

// decompress decompresses data according to the compression algorithm.
func decompress(data []byte, comp Compression, expectedSize, maxDecoderMemory uint64) ([]byte, error) {
	switch comp {
	case CompressionNone:
		if uint64(len(data)) != expectedSize {
			return nil, fmt.Errorf("%w: size mismatch", ErrDecompression)
		}
		return data, nil
	case CompressionZstd:
		dec, err := newZstdDecoder(bytes.NewReader(data), maxDecoderMemory)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDecompression, err)
		}
		defer dec.Close()
		content, err := readAllWithLimit(dec, expectedSize)
		if err != nil {
			if errors.Is(err, ErrSizeOverflow) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrDecompression, err)
		}
		if uint64(len(content)) != expectedSize {
			return nil, fmt.Errorf("%w: size mismatch", ErrDecompression)
		}
		return content, nil
	default:
		return nil, fmt.Errorf("unknown compression algorithm: %d", comp)
	}
}

// fileInfo implements fs.FileInfo for regular files.
type fileInfo struct {
	entry Entry
	name  string // base name only
	size  int64
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.size }
func (fi *fileInfo) Mode() fs.FileMode  { return fi.entry.Mode }
func (fi *fileInfo) ModTime() time.Time { return fi.entry.ModTime }
func (fi *fileInfo) IsDir() bool        { return false }
func (fi *fileInfo) Sys() any           { return nil }

func newFileInfo(entry Entry, name string) (*fileInfo, error) {
	size, err := sizeToInt64(entry.OriginalSize)
	if err != nil {
		return nil, err
	}
	return &fileInfo{entry: entry, name: name, size: size}, nil
}

// dirInfo implements fs.FileInfo for synthetic directories.
type dirInfo struct {
	name string
}

func (di *dirInfo) Name() string       { return di.name }
func (di *dirInfo) Size() int64        { return 0 }
func (di *dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (di *dirInfo) ModTime() time.Time { return time.Time{} }
func (di *dirInfo) IsDir() bool        { return true }
func (di *dirInfo) Sys() any           { return nil }

// dirEntry implements fs.DirEntry by wrapping fs.FileInfo.
type dirEntry struct {
	info    fs.FileInfo
	infoErr error
}

func (de *dirEntry) Name() string               { return de.info.Name() }
func (de *dirEntry) IsDir() bool                { return de.info.IsDir() }
func (de *dirEntry) Type() fs.FileMode          { return de.info.Mode().Type() }
func (de *dirEntry) Info() (fs.FileInfo, error) { return de.info, de.infoErr }

// file implements fs.File for regular files.
type file struct {
	r     *Reader
	entry Entry

	reader    io.Reader
	decoder   *zstd.Decoder
	hasher    hash.Hash
	remaining uint64

	initialized bool
	initErr     error
	verified    bool
	verifyErr   error
}

func (f *file) Read(p []byte) (int, error) {
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

	n, err := f.reader.Read(p)
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

func (f *file) Stat() (fs.FileInfo, error) {
	return newFileInfo(f.entry, pathBase(f.entry.Path))
}

func (f *file) Close() error {
	if err := f.init(); err != nil {
		return err
	}
	if f.decoder != nil {
		defer func() {
			f.decoder.Close()
			f.decoder = nil
		}()
	}
	if f.verified {
		return f.verifyErr
	}
	if !f.r.verifyOnClose {
		return nil
	}

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

func (f *file) init() error {
	if f.initialized {
		return f.initErr
	}
	f.initialized = true

	if err := validateEntry(&f.entry, f.r.source.Size(), f.r.maxFileSize); err != nil {
		f.initErr = fmt.Errorf("read %s: %w", f.entry.Path, err)
		return f.initErr
	}
	if err := validateEntryHash(&f.entry); err != nil {
		f.initErr = fmt.Errorf("read %s: %w", f.entry.Path, err)
		return f.initErr
	}

	if f.entry.Compression == CompressionNone && f.entry.DataSize != f.entry.OriginalSize {
		f.initErr = fmt.Errorf("%w: size mismatch", ErrDecompression)
		return f.initErr
	}

	offset, err := sizeToInt64(f.entry.DataOffset)
	if err != nil {
		f.initErr = err
		return f.initErr
	}
	length, err := sizeToInt64(f.entry.DataSize)
	if err != nil {
		f.initErr = err
		return f.initErr
	}
	section := io.NewSectionReader(f.r.source, offset, length)

	switch f.entry.Compression {
	case CompressionNone:
		f.reader = section
	case CompressionZstd:
		dec, err := newZstdDecoder(section, f.r.maxDecoderMemory)
		if err != nil {
			f.initErr = fmt.Errorf("%w: %v", ErrDecompression, err)
			return f.initErr
		}
		f.decoder = dec
		f.reader = dec
	default:
		f.initErr = fmt.Errorf("unknown compression algorithm: %d", f.entry.Compression)
		return f.initErr
	}

	f.remaining = f.entry.OriginalSize
	f.hasher = sha256.New()

	return nil
}

func (f *file) readExtra() (int, error) {
	var scratch [1]byte
	n, err := f.reader.Read(scratch[:])
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

func (f *file) verifyHash() error {
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

// openDir implements fs.File and fs.ReadDirFile for directories.
type openDir struct {
	r    *Reader
	name string
	iter *dirIter
}

func (d *openDir) Read(_ []byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}

func (d *openDir) Stat() (fs.FileInfo, error) {
	name := d.name
	if name == "." {
		name = "."
	} else {
		name = pathBase(d.name)
	}
	return &dirInfo{name: name}, nil
}

func (d *openDir) Close() error {
	if d.iter != nil {
		d.iter.Close()
		d.iter = nil
	}
	return nil
}

func (d *openDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.iter == nil {
		d.iter = newDirIter(d.r.index, dirPrefix(d.name))
	}

	if n <= 0 {
		return d.readAll()
	}

	entries := make([]fs.DirEntry, 0, n)
	for len(entries) < n {
		entry, ok := d.iter.Next()
		if !ok {
			if len(entries) == 0 {
				return nil, io.EOF
			}
			return entries, nil
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (d *openDir) readAll() ([]fs.DirEntry, error) {
	entries := make([]fs.DirEntry, 0)
	for {
		entry, ok := d.iter.Next()
		if !ok {
			return entries, nil
		}
		entries = append(entries, entry)
	}
}

// pathBase returns the last element of path (similar to filepath.Base but for slash-separated paths).
func pathBase(path string) string {
	if path == "" || path == "." {
		return "."
	}
	// Remove trailing slash if present
	path = strings.TrimSuffix(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// isDir checks if name is a directory (has entries under it).
func (r *Reader) isDir(name string) bool {
	if name == "." {
		return r.index.Len() > 0
	}
	prefix := name + "/"
	for range r.index.EntriesWithPrefix(prefix) {
		return true
	}
	return false
}

// readAndVerify reads file content from source, decompresses if needed, and verifies the hash.
func (r *Reader) readAndVerify(entry *Entry) ([]byte, error) {
	if err := validateEntry(entry, r.source.Size(), r.maxFileSize); err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}
	if err := validateEntryHash(entry); err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}

	if err := validateEntryCompression(entry); err != nil {
		return nil, err
	}

	section, err := r.sectionReader(entry)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}

	reader, closeFn, err := r.entryReader(entry, section)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	content, sum, err := readContentAndHash(entry, reader)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(sum, entry.Hash) {
		return nil, ErrHashMismatch
	}

	return content, nil
}

func validateEntry(entry *Entry, sourceSize int64, maxFileSize uint64) error {
	if sourceSize < 0 {
		return ErrSizeOverflow
	}

	if maxFileSize > 0 {
		if entry.DataSize > maxFileSize || entry.OriginalSize > maxFileSize {
			return ErrSizeOverflow
		}
	}

	end, ok := addUint64(entry.DataOffset, entry.DataSize)
	if !ok {
		return ErrSizeOverflow
	}
	if end > uint64(sourceSize) {
		return ErrSizeOverflow
	}

	return nil
}

func sizeToInt(size uint64) (int, error) {
	if size > uint64(math.MaxInt) {
		return 0, ErrSizeOverflow
	}
	return int(size), nil
}

func sizeToInt64(size uint64) (int64, error) {
	if size > uint64(math.MaxInt64) {
		return 0, ErrSizeOverflow
	}
	return int64(size), nil
}

func readAllWithLimit(r io.Reader, maxSize uint64) ([]byte, error) {
	if maxSize > uint64(math.MaxInt-1) {
		return nil, ErrSizeOverflow
	}
	limit := int64(maxSize) + 1 //nolint:gosec // checked above
	lr := &io.LimitedReader{R: r, N: limit}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > maxSize { //nolint:gosec // len is always non-negative
		return nil, ErrSizeOverflow
	}
	return data, nil
}

func addUint64(a, b uint64) (uint64, bool) {
	sum := a + b
	if sum < a {
		return 0, false
	}
	return sum, true
}

type dirIter struct {
	next     func() (Entry, bool)
	stop     func()
	prefix   string
	lastName string
	done     bool
}

func newDirIter(idx *Index, prefix string) *dirIter {
	next, stop := iter.Pull(idx.EntriesWithPrefix(prefix))
	return &dirIter{
		next:   next,
		stop:   stop,
		prefix: prefix,
	}
}

func (it *dirIter) Next() (fs.DirEntry, bool) {
	if it.done {
		return nil, false
	}
	for {
		entry, ok := it.next()
		if !ok {
			it.Close()
			return nil, false
		}

		childName, isSubDir := childFromPath(entry.Path, it.prefix)
		if childName == it.lastName {
			continue
		}
		it.lastName = childName

	if isSubDir {
		return &dirEntry{info: &dirInfo{name: childName}}, true
	}
	info, err := newFileInfo(entry, childName)
	if err != nil {
		info = &fileInfo{entry: entry, name: childName, size: 0}
	}
	return &dirEntry{info: info, infoErr: err}, true
}
}

func (it *dirIter) Close() {
	if it.done {
		return
	}
	it.done = true
	if it.stop != nil {
		it.stop()
		it.stop = nil
	}
}

func dirPrefix(name string) string {
	if name == "." {
		return ""
	}
	return name + "/"
}

func childFromPath(path, prefix string) (string, bool) {
	relPath := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(relPath, "/"); idx >= 0 {
		return relPath[:idx], true
	}
	return relPath, false
}

func newZstdDecoder(r io.Reader, maxDecoderMemory uint64) (*zstd.Decoder, error) {
	if maxDecoderMemory == 0 {
		return zstd.NewReader(r)
	}
	return zstd.NewReader(r, zstd.WithDecoderMaxMemory(maxDecoderMemory))
}

type hashingReader struct {
	r io.Reader
	h hash.Hash
}

func (hr *hashingReader) Read(p []byte) (int, error) {
	n, err := hr.r.Read(p)
	if n > 0 {
		_, _ = hr.h.Write(p[:n]) //nolint:errcheck // hash writes never fail
	}
	return n, err
}

func ensureNoExtra(r io.Reader) error {
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

func validateEntryCompression(entry *Entry) error {
	if entry.Compression == CompressionNone && entry.DataSize != entry.OriginalSize {
		return fmt.Errorf("%w: size mismatch", ErrDecompression)
	}
	return nil
}

func validateEntryHash(entry *Entry) error {
	if len(entry.Hash) != sha256.Size {
		return fmt.Errorf("invalid hash length: %d", len(entry.Hash))
	}
	return nil
}

func (r *Reader) sectionReader(entry *Entry) (*io.SectionReader, error) {
	offset, err := sizeToInt64(entry.DataOffset)
	if err != nil {
		return nil, err
	}
	length, err := sizeToInt64(entry.DataSize)
	if err != nil {
		return nil, err
	}
	return io.NewSectionReader(r.source, offset, length), nil
}

func (r *Reader) entryReader(entry *Entry, section *io.SectionReader) (io.Reader, func(), error) {
	switch entry.Compression {
	case CompressionNone:
		return section, func() {}, nil
	case CompressionZstd:
		dec, err := newZstdDecoder(section, r.maxDecoderMemory)
		if err != nil {
			return nil, func() {}, fmt.Errorf("%w: %v", ErrDecompression, err)
		}
		return dec, dec.Close, nil
	default:
		return nil, func() {}, fmt.Errorf("unknown compression algorithm: %d", entry.Compression)
	}
}

func readContentAndHash(entry *Entry, reader io.Reader) (content, sum []byte, err error) {
	contentSize, err := sizeToInt(entry.OriginalSize)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}
	content = make([]byte, contentSize)

	hasher := sha256.New()
	hr := &hashingReader{r: reader, h: hasher}
	n, err := io.ReadFull(hr, content)
	if err != nil {
		return nil, nil, mapReadError(entry, n, contentSize, err)
	}
	if err := ensureNoExtra(hr); err != nil {
		return nil, nil, err
	}

	return content, hasher.Sum(nil), nil
}

func mapReadError(entry *Entry, n, expected int, err error) error {
	if entry.Compression == CompressionNone {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("read %s: short read (%d of %d bytes)", entry.Path, n, expected)
		}
		return fmt.Errorf("read %s: %w", entry.Path, err)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: unexpected EOF", ErrDecompression)
	}
	return fmt.Errorf("%w: %v", ErrDecompression, err)
}
