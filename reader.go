package blob

import (
	"context"
	"io"
	"io/fs"
)

// ByteSource provides random access to the data blob.
//
// Implementations exist for local files (*os.File) and HTTP range requests.
type ByteSource interface {
	io.ReaderAt
	Size() int64
}

// Cache provides content-addressed storage for file contents.
//
// Keys are SHA256 hashes of uncompressed file content. Values are the
// uncompressed content. Because keys are content hashes, cache hits
// are implicitly verified—no additional integrity check is needed.
//
// Implementations should handle their own size limits and eviction policies.
type Cache interface {
	// Get retrieves content by its SHA256 hash.
	// Returns false if the content is not cached.
	Get(hash []byte) ([]byte, bool)

	// Put stores content indexed by its SHA256 hash.
	Put(hash []byte, content []byte) error
}

// ReaderOption configures a Reader.
type ReaderOption func(*Reader)

// WithCache configures the Reader to use a content-addressed cache.
//
// On cache hit, files are returned without network access or hash verification
// (the hash is the cache key, so correctness is implicit).
func WithCache(c Cache) ReaderOption {
	return func(r *Reader) {
		r.cache = c
	}
}

// WithContext configures the Reader to use the given context for cancellation.
//
// The context is used for all operations including Open, ReadFile, and Prefetch.
// If not set, context.Background() is used.
func WithContext(ctx context.Context) ReaderOption {
	return func(r *Reader) {
		r.ctx = ctx
	}
}

// Reader provides random access to archive files.
//
// Reader implements fs.FS, fs.StatFS, fs.ReadFileFS, and fs.ReadDirFS
// for compatibility with the standard library.
type Reader struct {
	index  *Index
	source ByteSource
	cache  Cache
	ctx    context.Context
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
		index:  index,
		source: source,
		ctx:    context.Background(),
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
	panic("not implemented")
}

// Stat implements fs.StatFS.
//
// Stat returns file info for the named file without reading its content.
// For directories (paths that are prefixes of other entries), Stat returns
// synthetic directory info.
func (r *Reader) Stat(name string) (fs.FileInfo, error) {
	panic("not implemented")
}

// ReadFile implements fs.ReadFileFS.
//
// ReadFile reads and returns the entire contents of the named file.
// The content is decompressed if necessary and verified against its hash.
//
// If a cache is configured and contains the file, ReadFile returns the
// cached content without network access.
func (r *Reader) ReadFile(name string) ([]byte, error) {
	panic("not implemented")
}

// ReadDir implements fs.ReadDirFS.
//
// ReadDir returns directory entries for the named directory, sorted by name.
// Directory entries are synthesized from file paths—the archive does not
// store directories explicitly.
func (r *Reader) ReadDir(name string) ([]fs.DirEntry, error) {
	panic("not implemented")
}

// Prefetch fetches and caches the specified files.
//
// For adjacent files, Prefetch batches range requests to minimize round trips.
// This is useful for warming the cache with files that will be accessed soon.
//
// Prefetch is a no-op if no cache is configured.
func (r *Reader) Prefetch(paths ...string) error {
	panic("not implemented")
}

// PrefetchDir fetches and caches all files under the given directory prefix.
//
// Because files are sorted by path and stored adjacently, PrefetchDir can
// fetch an entire directory's contents with a single range request, then
// split and cache each file individually.
//
// PrefetchDir is a no-op if no cache is configured.
func (r *Reader) PrefetchDir(prefix string) error {
	panic("not implemented")
}
