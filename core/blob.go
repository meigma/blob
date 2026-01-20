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
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"

	"golang.org/x/sync/singleflight"

	"github.com/meigma/blob/core/cache"
	"github.com/meigma/blob/core/internal/batch"
	"github.com/meigma/blob/core/internal/blobtype"
	"github.com/meigma/blob/core/internal/file"
	"github.com/meigma/blob/core/internal/index"
)

// Re-export types from internal/blobtype for public API.
type (
	// Entry represents a file in the archive.
	Entry = blobtype.Entry

	// Compression identifies the compression algorithm used for a file.
	Compression = blobtype.Compression

	// EntryView provides a read-only view of an index entry.
	EntryView = blobtype.EntryView

	// File represents an archive file with optional random access.
	// ReadAt is only supported for uncompressed entries.
	File interface {
		fs.File
		io.ReaderAt
	}
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
// SourceID must return a stable identifier for the underlying content.
type ByteSource interface {
	io.ReaderAt
	Size() int64
	SourceID() string
}

// Blob provides random access to archive files.
//
// Blob implements fs.FS, fs.StatFS, fs.ReadFileFS, and fs.ReadDirFS
// for compatibility with the standard library.
type Blob struct {
	idx                   *index.Index
	indexData             []byte
	reader                *file.Reader
	maxFileSize           uint64
	maxDecoderMemory      uint64
	decoderConcurrencySet bool
	decoderConcurrency    int
	decoderLowmemSet      bool
	decoderLowmem         bool
	verifyOnClose         bool
	cache                 cache.Cache        // nil = no caching
	readGroup             singleflight.Group // zero value is valid
	cacheGroup            singleflight.Group // zero value is valid
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
		maxFileSize:      file.DefaultMaxFileSize,
		maxDecoderMemory: file.DefaultMaxDecoderMemory,
		verifyOnClose:    true,
	}
	for _, opt := range opts {
		opt(b)
	}
	readerOpts := []file.Option{
		file.WithMaxFileSize(b.maxFileSize),
		file.WithMaxDecoderMemory(b.maxDecoderMemory),
	}
	if b.decoderConcurrencySet {
		readerOpts = append(readerOpts, file.WithDecoderConcurrency(b.decoderConcurrency))
	}
	if b.decoderLowmemSet {
		readerOpts = append(readerOpts, file.WithDecoderLowmem(b.decoderLowmem))
	}
	b.reader = file.NewReader(source, readerOpts...)
	return b, nil
}

// Open implements fs.FS.
//
// Open returns an fs.File for reading the named file. The returned file
// verifies the content hash on Close (unless disabled by WithVerifyOnClose)
// and returns ErrHashMismatch if verification fails. Callers must read to
// EOF or Close to ensure integrity; partial reads may return unverified data.
//
// When caching is enabled (via WithCache), cached content is verified while
// reading and may return ErrHashMismatch if the cache was corrupted.
func (b *Blob) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	if view, ok := b.idx.LookupView(name); ok {
		entry := blobtype.EntryFromViewWithPath(view, name)

		// No cache - existing behavior
		if b.cache == nil {
			return b.reader.OpenFile(&entry, b.verifyOnClose), nil
		}

		// Cache hit - return file from cache
		if f, ok := b.cache.Get(entry.Hash); ok {
			return newCachedFile(f, &entry, b.verifyOnClose, b.cache.Delete), nil
		}

		// Cache miss - populate then return from cache
		if err := b.ensureCached(&entry); err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}

		if f, ok := b.cache.Get(entry.Hash); ok {
			return newCachedFile(f, &entry, b.verifyOnClose, b.cache.Delete), nil
		}
		return b.reader.OpenFile(&entry, b.verifyOnClose), nil
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
		info, err := file.NewInfo(&entry, file.Base(name))
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
		}
		return info, nil
	}

	// Check if it's a directory
	if b.isDir(name) {
		dirName := file.Base(name)
		if name == "." {
			dirName = "."
		}
		return file.NewDirInfo(dirName), nil
	}

	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// ReadFile implements fs.ReadFileFS.
//
// ReadFile reads and returns the entire contents of the named file.
// The content is decompressed if necessary and verified against its hash.
//
// When caching is enabled, concurrent calls for the same content are
// deduplicated using singleflight, preventing redundant network requests.
func (b *Blob) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	view, ok := b.idx.LookupView(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}

	entry := blobtype.EntryFromViewWithPath(view, name)

	// No cache - existing behavior
	if b.cache == nil {
		return b.reader.ReadAll(&entry)
	}

	// Cache hit - read from cached file
	if f, ok := b.cache.Get(entry.Hash); ok {
		defer f.Close()
		hasher := sha256.New()
		content, err := io.ReadAll(io.TeeReader(f, hasher))
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(hasher.Sum(nil), entry.Hash) {
			_ = b.cache.Delete(entry.Hash) //nolint:errcheck // best-effort cache cleanup on hash mismatch
			return nil, ErrHashMismatch
		}
		return content, nil
	}

	// Cache miss with singleflight
	result, err, _ := b.readGroup.Do(string(entry.Hash), func() (any, error) {
		// Double-check cache
		if f, ok := b.cache.Get(entry.Hash); ok {
			defer f.Close()
			return io.ReadAll(f)
		}

		// Read into memory (we need []byte anyway)
		content, err := b.reader.ReadAll(&entry)
		if err != nil {
			return nil, err
		}

		// Store in cache (errors are non-fatal)
		_ = b.cache.Put(entry.Hash, &bytesFile{ //nolint:errcheck // caching is opportunistic
			Reader: bytes.NewReader(content),
			size:   int64(len(content)),
		})

		return content, nil
	})

	if err != nil {
		return nil, err
	}
	return result.([]byte), nil //nolint:errcheck // type assertion always succeeds when err is nil
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

	prefix := file.DirPrefix(name)
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

// Reader returns the underlying file reader.
// This is useful for cached readers that need to share the decompression pool.
func (b *Blob) Reader() *file.Reader {
	return b.reader
}

// IndexData returns the raw FlatBuffers-encoded index data.
// This is useful for creating new Blobs with different data sources.
func (b *Blob) IndexData() []byte {
	return b.indexData
}

// DataHash returns the hash of the data blob bytes from the index.
// The returned slice aliases the index buffer and must be treated as immutable.
// ok is false when the index did not record data metadata.
func (b *Blob) DataHash() ([]byte, bool) {
	return b.idx.DataHash()
}

// DataSize returns the size of the data blob in bytes from the index.
// ok is false when the index did not record data metadata.
func (b *Blob) DataSize() (uint64, bool) {
	return b.idx.DataSize()
}

// Stream returns a reader that streams the entire data blob from beginning to end.
// This is useful for copying or transmitting the complete data content.
func (b *Blob) Stream() io.Reader {
	return io.NewSectionReader(b.reader.Source(), 0, b.reader.Source().Size())
}

// Size returns the total size of the data blob in bytes.
func (b *Blob) Size() int64 {
	return b.reader.Source().Size()
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

// CopyTo extracts specific files to a destination directory.
//
// Parent directories are created as needed.
//
// By default:
//   - Existing files are skipped (use CopyWithOverwrite to overwrite)
//   - File modes and times are not preserved (use CopyWithPreserveMode/Times)
//   - Range reads are pipelined (when beneficial) with concurrency 4 (use CopyWithReadConcurrency to change)
func (b *Blob) CopyTo(destDir string, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}

	cfg := copyConfig{}
	return b.copyEntries(destDir, b.collectPathEntries(paths), &cfg)
}

// CopyToWithOptions extracts specific files with options.
func (b *Blob) CopyToWithOptions(destDir string, paths []string, opts ...CopyOption) error {
	if len(paths) == 0 {
		return nil
	}

	cfg := copyConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.cleanDest {
		return errors.New("CopyWithCleanDest is only supported by CopyDir")
	}
	return b.copyEntries(destDir, b.collectPathEntries(paths), &cfg)
}

// CopyDir extracts all files under a directory prefix to a destination.
//
// If prefix is "" or ".", all files in the archive are extracted.
//
// Files are written atomically using temp files and renames by default.
// CopyWithCleanDest clears the destination prefix and writes directly
// to the final path. This is more performant but less safe.
//
// Parent directories are created as needed.
//
// By default:
//   - Existing files are skipped (use CopyWithOverwrite to overwrite)
//   - File modes and times are not preserved (use CopyWithPreserveMode/Times)
//   - Range reads are pipelined (when beneficial) with concurrency 4 (use CopyWithReadConcurrency to change)
func (b *Blob) CopyDir(destDir, prefix string, opts ...CopyOption) error {
	cfg := copyConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.cleanDest {
		target, err := cleanCopyDest(destDir, prefix)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("clean destination %s: %w", target, err)
		}
		cfg.overwrite = true
	}
	return b.copyEntries(destDir, b.collectPrefixEntries(prefix), &cfg)
}

// collectPathEntries collects entries for specific paths.
func (b *Blob) collectPathEntries(paths []string) []*batch.Entry {
	entries := make([]*batch.Entry, 0, len(paths))
	for _, path := range paths {
		if !fs.ValidPath(path) {
			continue
		}
		view, ok := b.idx.LookupView(path)
		if !ok {
			continue
		}
		entry := blobtype.EntryFromViewWithPath(view, path)
		entries = append(entries, &entry)
	}
	return entries
}

// collectPrefixEntries collects all entries under a prefix.
func (b *Blob) collectPrefixEntries(prefix string) []*batch.Entry {
	if prefix != "" && prefix != "." && !fs.ValidPath(prefix) {
		return nil
	}

	var dirPrefix string
	if prefix == "" || prefix == "." {
		dirPrefix = ""
	} else {
		dirPrefix = file.DirPrefix(prefix)
	}

	var entries []*batch.Entry //nolint:prealloc // size unknown until iteration
	for view := range b.idx.EntriesWithPrefixView(dirPrefix) {
		entry := blobtype.EntryFromViewWithPath(view, view.Path())
		entries = append(entries, &entry)
	}
	return entries
}

// copyEntries uses the batch processor to copy entries to destDir.
func (b *Blob) copyEntries(destDir string, entries []*batch.Entry, cfg *copyConfig) error {
	if len(entries) == 0 {
		return nil
	}

	// Create file sink with options
	sinkOpts := []batch.FileSinkOption{
		batch.WithOverwrite(cfg.overwrite),
		batch.WithPreserveMode(cfg.preserveMode),
		batch.WithPreserveTimes(cfg.preserveTimes),
	}
	if cfg.cleanDest {
		sinkOpts = append(sinkOpts, batch.WithDirectWrites(true))
	}
	sink := batch.NewFileSink(destDir, sinkOpts...)

	// Create processor with options
	var procOpts []batch.ProcessorOption
	if cfg.workers != 0 {
		procOpts = append(procOpts, batch.WithWorkers(cfg.workers))
	}
	readConcurrency := cfg.readConcurrency
	if !cfg.readConcurrencySet {
		readConcurrency = defaultCopyReadConcurrency
	}
	if readConcurrency != 0 {
		procOpts = append(procOpts, batch.WithReadConcurrency(readConcurrency))
	}
	if cfg.readAheadBytesSet {
		procOpts = append(procOpts, batch.WithReadAheadBytes(cfg.readAheadBytes))
	}
	proc := batch.NewProcessor(b.reader.Source(), b.reader.Pool(), b.maxFileSize, procOpts...)

	return proc.Process(entries, sink)
}

func cleanCopyDest(destDir, prefix string) (string, error) {
	if destDir == "" {
		return "", errors.New("clean destination: destDir is empty")
	}
	if prefix != "" && prefix != "." && !fs.ValidPath(prefix) {
		return "", fmt.Errorf("clean destination: invalid prefix %q", prefix)
	}

	target := destDir
	if prefix != "" && prefix != "." {
		target = filepath.Join(destDir, filepath.FromSlash(prefix))
	}
	target = filepath.Clean(target)
	if target == "" || target == "." || target == string(filepath.Separator) {
		return "", fmt.Errorf("clean destination: refusing to remove %q", target)
	}

	volume := filepath.VolumeName(target)
	if volume != "" {
		if target == volume || target == volume+string(filepath.Separator) {
			return "", fmt.Errorf("clean destination: refusing to remove %q", target)
		}
	}

	return target, nil
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
		name = file.Base(d.name)
	}
	return file.NewDirInfo(name), nil
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
		d.iter = newDirIter(d.b.idx, file.DirPrefix(d.name))
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
		childName, isSubDir := file.Child(path, it.prefix)
		if childName == it.lastName {
			continue
		}
		it.lastName = childName

		if isSubDir {
			return file.NewDirEntry(file.NewDirInfo(childName), nil), true
		}
		entry := blobtype.EntryFromViewWithPath(view, path)
		info, err := file.NewInfo(&entry, childName)
		if err != nil {
			// Return a fallback info with size 0
			info = &file.Info{}
		}
		return file.NewDirEntry(info, err), true
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
