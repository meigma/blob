package blob

import (
	"io"
	"io/fs"
	"iter"

	"github.com/meigma/blob/internal/fileops"
	"github.com/meigma/blob/internal/pathutil"
)

// ByteSource provides random access to the data blob.
//
// Implementations exist for local files (*os.File) and HTTP range requests.
type ByteSource interface {
	io.ReaderAt
	Size() int64
}

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
	ops              *fileops.Ops
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
		maxFileSize:      fileops.DefaultMaxFileSize,
		maxDecoderMemory: fileops.DefaultMaxDecoderMemory,
		verifyOnClose:    true,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.ops = fileops.New(
		source,
		fileops.WithMaxFileSize(r.maxFileSize),
		fileops.WithMaxDecoderMemory(r.maxDecoderMemory),
	)
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
	if view, ok := r.index.LookupView(name); ok {
		entry := entryFromViewWithPath(view, name)
		return r.ops.OpenFile(&entry, r.verifyOnClose), nil
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
	if view, ok := r.index.LookupView(name); ok {
		entry := entryFromViewWithPath(view, name)
		info, err := fileops.NewFileInfo(&entry, pathutil.Base(name))
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
		}
		return info, nil
	}

	// Check if it's a directory
	if r.isDir(name) {
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
func (r *Reader) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	view, ok := r.index.LookupView(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}

	entry := entryFromViewWithPath(view, name)
	return r.ops.ReadAll(&entry)
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

	prefix := pathutil.DirPrefix(name)
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

// Ops returns the underlying file operations helper.
// This is useful for cached readers that need to share the decompression pool.
func (r *Reader) Ops() *fileops.Ops {
	return r.ops
}

// Index returns the underlying index for lookups.
// This is useful for cached readers that need direct index access.
func (r *Reader) Index() *Index {
	return r.index
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
		d.iter = newDirIter(d.r.index, pathutil.DirPrefix(d.name))
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
func (r *Reader) isDir(name string) bool {
	if name == "." {
		return r.index.Len() > 0
	}
	prefix := name + "/"
	for range r.index.EntriesWithPrefixView(prefix) {
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

func newDirIter(idx *Index, prefix string) *dirIter {
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
		entry := entryFromViewWithPath(view, path)
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
