package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"math"
	"sort"
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

// ReaderOption configures a Reader.
type ReaderOption func(*Reader)

// WithContext configures the Reader to use the given context for cancellation.
//
// The context is used for all read operations.
// If not set, context.Background() is used.
func WithContext(ctx context.Context) ReaderOption {
	return func(r *Reader) {
		r.ctx = ctx
	}
}

// WithMaxFileSize limits the maximum per-file size (compressed and uncompressed).
// Set limit to 0 to disable the limit.
func WithMaxFileSize(limit uint64) ReaderOption {
	return func(r *Reader) {
		r.maxFileSize = limit
	}
}

// Reader provides random access to archive files.
//
// Reader implements fs.FS, fs.StatFS, fs.ReadFileFS, and fs.ReadDirFS
// for compatibility with the standard library.
type Reader struct {
	index       *Index
	source      ByteSource
	ctx         context.Context
	maxFileSize uint64
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
// Options can be used to configure caching and cancellation.
func NewReader(index *Index, source ByteSource, opts ...ReaderOption) *Reader {
	r := &Reader{
		index:       index,
		source:      source,
		ctx:         context.Background(),
		maxFileSize: defaultMaxFileSize,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Open implements fs.FS.
//
// Open returns an fs.File for reading the named file. The returned file
// verifies the content hash on Close and returns ErrHashMismatch if
// verification fails.
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
		return &fileInfo{entry: entry, name: pathBase(name)}, nil
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

	// Build prefix for matching
	var prefix string
	if name == "." {
		prefix = ""
	} else {
		prefix = name + "/"
	}

	// Track unique immediate children (files and directories)
	seen := make(map[string]fs.FileInfo)

	for entry := range r.index.EntriesWithPrefix(prefix) {
		// Get path relative to directory
		relPath := strings.TrimPrefix(entry.Path, prefix)

		// Extract immediate child name
		var childName string
		var isSubDir bool
		if idx := strings.Index(relPath, "/"); idx >= 0 {
			childName = relPath[:idx]
			isSubDir = true
		} else {
			childName = relPath
			isSubDir = false
		}

		// Skip if already seen
		if _, ok := seen[childName]; ok {
			continue
		}

		if isSubDir {
			// Synthetic directory
			seen[childName] = &dirInfo{name: childName}
		} else {
			// File
			seen[childName] = &fileInfo{entry: entry, name: childName}
		}
	}

	// Check if directory exists
	if len(seen) == 0 && name != "." {
		// Directory doesn't exist (no entries under it)
		if !r.isDir(name) {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
		}
	}

	// Convert to sorted slice
	entries := make([]fs.DirEntry, 0, len(seen))
	for _, info := range seen {
		entries = append(entries, &dirEntry{info: info})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

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
func decompress(data []byte, comp Compression, expectedSize uint64) ([]byte, error) {
	switch comp {
	case CompressionNone:
		if uint64(len(data)) != expectedSize {
			return nil, fmt.Errorf("%w: size mismatch", ErrDecompression)
		}
		return data, nil
	case CompressionZstd:
		dec, err := zstd.NewReader(bytes.NewReader(data))
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
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return int64(fi.entry.OriginalSize) } //nolint:gosec // size fits in int64
func (fi *fileInfo) Mode() fs.FileMode  { return fi.entry.Mode }
func (fi *fileInfo) ModTime() time.Time { return fi.entry.ModTime }
func (fi *fileInfo) IsDir() bool        { return false }
func (fi *fileInfo) Sys() any           { return nil }

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
	info fs.FileInfo
}

func (de *dirEntry) Name() string               { return de.info.Name() }
func (de *dirEntry) IsDir() bool                { return de.info.IsDir() }
func (de *dirEntry) Type() fs.FileMode          { return de.info.Mode().Type() }
func (de *dirEntry) Info() (fs.FileInfo, error) { return de.info, nil }

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
	return &fileInfo{entry: f.entry, name: pathBase(f.entry.Path)}, nil
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
		dec, err := zstd.NewReader(section)
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
	r       *Reader
	name    string
	entries []fs.DirEntry
	offset  int
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
	return nil
}

func (d *openDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.entries == nil {
		entries, err := d.r.ReadDir(d.name)
		if err != nil {
			return nil, err
		}
		d.entries = entries
	}

	if n <= 0 {
		entries := d.entries[d.offset:]
		d.offset = len(d.entries)
		return entries, nil
	}

	if d.offset >= len(d.entries) {
		return nil, io.EOF
	}

	end := min(d.offset+n, len(d.entries))

	entries := d.entries[d.offset:end]
	d.offset = end
	return entries, nil
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

	// Read compressed/raw bytes from source
	dataSize, err := sizeToInt(entry.DataSize)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}
	data := make([]byte, dataSize)
	n, err := r.source.ReadAt(data, int64(entry.DataOffset)) //nolint:gosec // offset fits in int64
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}
	if uint64(n) != entry.DataSize { //nolint:gosec // n is always non-negative
		return nil, fmt.Errorf("read %s: short read (%d of %d bytes)", entry.Path, n, entry.DataSize)
	}

	// Decompress if needed
	content, err := decompress(data, entry.Compression, entry.OriginalSize)
	if err != nil {
		return nil, err
	}

	// Verify hash
	sum := sha256.Sum256(content)
	if !bytes.Equal(sum[:], entry.Hash) {
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
