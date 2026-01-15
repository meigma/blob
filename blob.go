//go:generate flatc --go --go-namespace fb -o internal schema/index.fbs

// Package blob provides a file archive format optimized for random access
// via HTTP range requests against OCI registries.
//
// Archives consist of two OCI blobs:
//   - Index blob: FlatBuffers-encoded file metadata enabling O(log n) lookups
//   - Data blob: Concatenated file contents, sorted by path for efficient directory fetches
//
// The package implements fs.FS and related interfaces for stdlib compatibility.
package blob

import (
	"errors"
	"io"
	"io/fs"
	"iter"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/fileops"
	"github.com/meigma/blob/internal/index"
	"github.com/meigma/blob/internal/pathutil"
)

// Re-export types from internal/blobtype for public API.
type (
	// Entry represents a file in the archive.
	Entry = blobtype.Entry

	// Compression identifies the compression algorithm used for a file.
	Compression = blobtype.Compression

	// EntryView provides a read-only view of an index entry.
	EntryView = blobtype.EntryView
)

// EntryFromViewWithPath creates an Entry from an EntryView with the given path.
var EntryFromViewWithPath = blobtype.EntryFromViewWithPath

// Re-export compression constants.
const (
	CompressionNone = blobtype.CompressionNone
	CompressionZstd = blobtype.CompressionZstd
)

// Interface compliance.
var (
	_ fs.FS         = (*Blob)(nil)
	_ fs.StatFS     = (*Blob)(nil)
	_ fs.ReadFileFS = (*Blob)(nil)
	_ fs.ReadDirFS  = (*Blob)(nil)
)

// Sentinel errors re-exported from internal/blobtype.
var (
	// ErrHashMismatch is returned when file content does not match its hash.
	ErrHashMismatch = blobtype.ErrHashMismatch

	// ErrDecompression is returned when decompression fails.
	ErrDecompression = blobtype.ErrDecompression

	// ErrSizeOverflow is returned when byte counts exceed supported limits.
	ErrSizeOverflow = blobtype.ErrSizeOverflow
)

// Sentinel errors specific to the blob package.
var (
	// ErrSymlink is returned when a symlink is encountered where not allowed.
	ErrSymlink = errors.New("blob: symlink")

	// ErrTooManyFiles is returned when the file count exceeds the configured limit.
	ErrTooManyFiles = errors.New("blob: too many files")
)

// ByteSource provides random access to the data blob.
//
// Implementations exist for local files (*os.File) and HTTP range requests.
type ByteSource interface {
	io.ReaderAt
	Size() int64
}

// Option configures a Blob.
type Option func(*Blob)

// WithMaxFileSize limits the maximum per-file size (compressed and uncompressed).
// Set limit to 0 to disable the limit.
func WithMaxFileSize(limit uint64) Option {
	return func(b *Blob) {
		b.maxFileSize = limit
	}
}

// WithMaxDecoderMemory limits the maximum memory used by the zstd decoder.
// Set limit to 0 to disable the limit.
func WithMaxDecoderMemory(limit uint64) Option {
	return func(b *Blob) {
		b.maxDecoderMemory = limit
	}
}

// WithVerifyOnClose controls whether Close drains the file to verify the hash.
//
// When false, Close returns without reading the remaining data. Integrity is
// only guaranteed when callers read to EOF.
func WithVerifyOnClose(enabled bool) Option {
	return func(b *Blob) {
		b.verifyOnClose = enabled
	}
}

// Blob provides random access to archive files.
//
// Blob implements fs.FS, fs.StatFS, fs.ReadFileFS, and fs.ReadDirFS
// for compatibility with the standard library.
type Blob struct {
	idx              *index.Index
	indexData        []byte
	ops              *fileops.Ops
	maxFileSize      uint64
	maxDecoderMemory uint64
	verifyOnClose    bool
}

// New creates a Blob for accessing files in the archive.
//
// The indexData is the FlatBuffers-encoded index blob and source provides
// access to file content. Options can be used to configure size and decoder limits.
func New(indexData []byte, source ByteSource, opts ...Option) (*Blob, error) {
	idx, err := index.Load(indexData)
	if err != nil {
		return nil, err
	}

	b := &Blob{
		idx:              idx,
		indexData:        indexData,
		maxFileSize:      fileops.DefaultMaxFileSize,
		maxDecoderMemory: fileops.DefaultMaxDecoderMemory,
		verifyOnClose:    true,
	}
	for _, opt := range opts {
		opt(b)
	}
	b.ops = fileops.New(
		source,
		fileops.WithMaxFileSize(b.maxFileSize),
		fileops.WithMaxDecoderMemory(b.maxDecoderMemory),
	)
	return b, nil
}

// Open implements fs.FS.
//
// Open returns an fs.File for reading the named file. The returned file
// verifies the content hash on Close (unless disabled by WithVerifyOnClose)
// and returns ErrHashMismatch if verification fails. Callers must read to
// EOF or Close to ensure integrity; partial reads may return unverified data.
func (b *Blob) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	if view, ok := b.idx.LookupView(name); ok {
		entry := blobtype.EntryFromViewWithPath(view, name)
		return b.ops.OpenFile(&entry, b.verifyOnClose), nil
	}

	// Check if it's a directory
	if b.isDir(name) {
		return &openDir{b: b, name: name}, nil
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// Stat implements fs.StatFS.
//
// Stat returns file info for the named file without reading its content.
// For directories (paths that are prefixes of other entries), Stat returns
// synthetic directory info.
func (b *Blob) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	if view, ok := b.idx.LookupView(name); ok {
		entry := blobtype.EntryFromViewWithPath(view, name)
		info, err := fileops.NewFileInfo(&entry, pathutil.Base(name))
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
		}
		return info, nil
	}

	// Check if it's a directory
	if b.isDir(name) {
		dirName := pathutil.Base(name)
		if name == "." {
			dirName = "."
		}
		return fileops.NewDirInfo(dirName), nil
	}

	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// ReadFile implements fs.ReadFileFS.
//
// ReadFile reads and returns the entire contents of the named file.
// The content is decompressed if necessary and verified against its hash.
func (b *Blob) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	view, ok := b.idx.LookupView(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}

	entry := blobtype.EntryFromViewWithPath(view, name)
	return b.ops.ReadAll(&entry)
}

// ReadDir implements fs.ReadDirFS.
//
// ReadDir returns directory entries for the named directory, sorted by name.
// Directory entries are synthesized from file paths—the archive does not
// store directories explicitly.
func (b *Blob) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	prefix := pathutil.DirPrefix(name)
	di := newDirIter(b.idx, prefix)
	defer di.Close()

	entries := make([]fs.DirEntry, 0)
	for {
		entry, ok := di.Next()
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

// Ops returns the underlying file operations helper.
// This is useful for cached readers that need to share the decompression pool.
func (b *Blob) Ops() *fileops.Ops {
	return b.ops
}

// IndexData returns the raw FlatBuffers-encoded index data.
// This is useful for creating new Blobs with different data sources.
func (b *Blob) IndexData() []byte {
	return b.indexData
}

// Entry returns a read-only view of the entry for the given path.
//
// The returned view is only valid while the Blob remains alive.
func (b *Blob) Entry(path string) (EntryView, bool) {
	return b.idx.LookupView(path)
}

// Entries returns an iterator over all entries as read-only views.
//
// The returned views are only valid while the Blob remains alive.
func (b *Blob) Entries() iter.Seq[EntryView] {
	return b.idx.EntriesView()
}

// EntriesWithPrefix returns an iterator over entries with the given prefix
// as read-only views.
//
// The returned views are only valid while the Blob remains alive.
func (b *Blob) EntriesWithPrefix(prefix string) iter.Seq[EntryView] {
	return b.idx.EntriesWithPrefixView(prefix)
}

// Len returns the number of entries in the archive.
func (b *Blob) Len() int {
	return b.idx.Len()
}

// openDir implements fs.File and fs.ReadDirFile for directories.
type openDir struct {
	b    *Blob
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
		name = pathutil.Base(d.name)
	}
	return fileops.NewDirInfo(name), nil
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
		d.iter = newDirIter(d.b.idx, pathutil.DirPrefix(d.name))
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

// isDir checks if name is a directory (has entries under it).
func (b *Blob) isDir(name string) bool {
	if name == "." {
		return b.idx.Len() > 0
	}
	prefix := name + "/"
	for range b.idx.EntriesWithPrefixView(prefix) {
		return true
	}
	return false
}

// dirIter iterates over directory entries, synthesizing subdirectories.
type dirIter struct {
	next     func() (EntryView, bool)
	stop     func()
	prefix   string
	lastName string
	done     bool
}

func newDirIter(idx *index.Index, prefix string) *dirIter {
	next, stop := iter.Pull(idx.EntriesWithPrefixView(prefix))
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
		view, ok := it.next()
		if !ok {
			it.Close()
			return nil, false
		}

		path := string(view.PathBytes())
		childName, isSubDir := pathutil.Child(path, it.prefix)
		if childName == it.lastName {
			continue
		}
		it.lastName = childName

		if isSubDir {
			return fileops.NewDirEntry(fileops.NewDirInfo(childName), nil), true
		}
		entry := blobtype.EntryFromViewWithPath(view, path)
		info, err := fileops.NewFileInfo(&entry, childName)
		if err != nil {
			// Return a fallback info with size 0
			info = &fileops.FileInfo{}
		}
		return fileops.NewDirEntry(info, err), true
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
