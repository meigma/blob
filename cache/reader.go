package cache

import (
	"bytes"
	"io/fs"
	"math"
	"runtime"

	"golang.org/x/sync/singleflight"

	"github.com/meigma/blob"
	"github.com/meigma/blob/internal/batch"
	"github.com/meigma/blob/internal/fileops"
)

// Reader wraps a blob.Reader with content-addressed caching.
//
// Reader implements the same fs.FS interfaces as blob.Reader, but checks
// the cache before fetching from the underlying source and caches content
// after successful reads.
//
// Prefetch/PrefetchDir use a size-based heuristic for parallelism when unset;
// override with WithPrefetchConcurrency to force serial or parallel behavior.
//
// For streaming reads via Open(), caching behavior depends on the cache type:
//   - StreamingCache: content streams to cache without full buffering
//   - Basic Cache: content is buffered in memory then cached on Close
//
// Reader uses singleflight to deduplicate concurrent ReadFile calls
// for the same content, preventing redundant network requests during
// cache miss storms.
type Reader struct {
	base            *blob.Reader
	cache           Cache
	prefetchWorkers int
	fetchGroup      singleflight.Group
}

// Interface compliance.
var (
	_ fs.FS         = (*Reader)(nil)
	_ fs.StatFS     = (*Reader)(nil)
	_ fs.ReadFileFS = (*Reader)(nil)
	_ fs.ReadDirFS  = (*Reader)(nil)
)

// ReaderOption configures a Reader.
type ReaderOption func(*Reader)

// WithPrefetchConcurrency sets the number of workers used for Prefetch/PrefetchDir.
// Values < 0 force serial execution. Zero uses a size-based heuristic.
// Values > 0 force a fixed worker count.
func WithPrefetchConcurrency(workers int) ReaderOption {
	return func(r *Reader) {
		r.prefetchWorkers = workers
	}
}

// NewReader wraps a blob.Reader with caching support.
func NewReader(base *blob.Reader, cache Cache, opts ...ReaderOption) *Reader {
	r := &Reader{
		base:  base,
		cache: cache,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(r)
	}
	return r
}

// Open implements fs.FS with caching support.
//
// For files, the returned fs.File will cache content after successful
// hash verification on Close. For StreamingCache implementations, content
// streams directly to the cache. For basic Cache implementations, content
// is buffered in memory.
func (r *Reader) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	view, ok := r.base.Index().LookupView(name)
	if !ok {
		// Not a file, delegate to base (handles directories)
		return r.base.Open(name)
	}
	entry := entryFromViewWithPath(view, name)

	// Check cache first
	if content, cached := r.cache.Get(entry.Hash); cached {
		return &cachedContentFile{
			entry:   entryToFileops(entry),
			content: content,
		}, nil
	}

	// Open base file and wrap for caching
	baseFile, err := r.base.Open(name)
	if err != nil {
		return nil, err
	}

	f, ok := baseFile.(*fileops.File)
	if !ok {
		// Should not happen for files, but return base file if it does
		return baseFile, nil
	}

	return r.wrapFileForCaching(f, entry)
}

// Stat implements fs.StatFS.
func (r *Reader) Stat(name string) (fs.FileInfo, error) {
	return r.base.Stat(name)
}

// ReadFile implements fs.ReadFileFS with caching support.
//
// ReadFile checks the cache first and returns cached content if available.
// On cache miss, it reads from the source and caches the result.
//
// Concurrent calls for the same content are deduplicated using singleflight,
// so only one network request is made even if multiple goroutines request
// the same file simultaneously.
func (r *Reader) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	view, ok := r.base.Index().LookupView(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	entry := entryFromViewWithPath(view, name)

	// Check cache first (fast path, avoids singleflight overhead)
	if content, cached := r.cache.Get(entry.Hash); cached {
		return content, nil
	}

	// Use content hash as singleflight key. All concurrent callers requesting
	// the same content (even via different paths) share a single fetch.
	key := string(entry.Hash)

	result, err, _ := r.fetchGroup.Do(key, func() (any, error) {
		// Double-check cache: another goroutine may have just cached this
		// content between our cache check and acquiring the singleflight lock.
		if content, cached := r.cache.Get(entry.Hash); cached {
			return content, nil
		}

		// Fetch from source
		content, err := r.base.ReadFile(name)
		if err != nil {
			return nil, err
		}

		// Cache the content (errors are non-fatal)
		_ = r.cache.Put(entry.Hash, content) //nolint:errcheck // caching is opportunistic

		return content, nil
	})

	if err != nil {
		return nil, err
	}

	content, _ := result.([]byte) //nolint:errcheck // type assertion always succeeds when err is nil
	return content, nil
}

// ReadDir implements fs.ReadDirFS.
func (r *Reader) ReadDir(name string) ([]fs.DirEntry, error) {
	return r.base.ReadDir(name)
}

// Prefetch fetches and caches the specified files.
//
// For adjacent files, Prefetch batches range requests to minimize round trips.
// This is useful for warming the cache with files that will be accessed soon.
func (r *Reader) Prefetch(paths ...string) error {
	return r.prefetchEntries(r.collectEntriesForPaths(paths))
}

// PrefetchDir fetches and caches all files under the given directory prefix.
//
// Because files are sorted by path and stored adjacently, PrefetchDir can
// fetch an entire directory's contents with a single range request, then
// split and cache each file individually.
func (r *Reader) PrefetchDir(prefix string) error {
	if !fs.ValidPath(prefix) && prefix != "" {
		return nil
	}

	// Collect all entries under prefix
	var dirPrefix string
	if prefix == "" || prefix == "." {
		dirPrefix = ""
	} else {
		dirPrefix = prefix + "/"
	}

	return r.prefetchEntries(r.collectEntriesWithPrefix(dirPrefix))
}

func (r *Reader) collectEntriesForPaths(paths []string) []*batch.Entry {
	entries := make([]batch.Entry, 0, len(paths))
	for _, path := range paths {
		if !fs.ValidPath(path) {
			continue
		}
		view, ok := r.base.Index().LookupView(path)
		if !ok {
			continue
		}
		entry := entryFromViewWithPath(view, path)
		entries = append(entries, entry)
	}
	return entryPointers(entries)
}

func (r *Reader) collectEntriesWithPrefix(prefix string) []*batch.Entry {
	var entries []batch.Entry //nolint:prealloc // size unknown until iteration
	for view := range r.base.Index().EntriesWithPrefixView(prefix) {
		entry := entryFromViewWithPath(view, "")
		entries = append(entries, entry)
	}
	return entryPointers(entries)
}

func entryPointers(entries []batch.Entry) []*batch.Entry {
	if len(entries) == 0 {
		return nil
	}
	ptrs := make([]*batch.Entry, len(entries))
	for i := range entries {
		ptrs[i] = &entries[i]
	}
	return ptrs
}
func (r *Reader) prefetchEntries(entries []*batch.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	workers := r.prefetchWorkers
	if workers == 0 {
		if _, ok := r.cache.(StreamingCache); !ok {
			workers = runtime.GOMAXPROCS(0)
		}
	}

	var procOpts []batch.ProcessorOption
	if workers != 0 {
		procOpts = append(procOpts, batch.WithWorkers(workers))
	}
	proc := batch.NewProcessor(r.base.Ops().Source(), r.base.Ops().Pool(), r.base.Ops().MaxFileSize(), procOpts...)

	var sink batch.Sink = &cacheSink{cache: r.cache}
	if _, ok := r.cache.(StreamingCache); !ok {
		sink = &nonStreamingCacheSink{cacheSink: &cacheSink{cache: r.cache}}
	}
	return proc.Process(entries, sink)
}

// wrapFileForCaching wraps a base file with caching support.
//
//nolint:gocritic // hugeParam acceptable for Entry value semantics
func (r *Reader) wrapFileForCaching(f *fileops.File, entry blob.Entry) (fs.File, error) {
	fileopsEntry := entryToFileops(entry)

	// Check if cache supports streaming
	if sc, ok := r.cache.(StreamingCache); ok {
		w, err := sc.Writer(entry.Hash)
		if err != nil {
			// Fall back to buffered caching
			return r.wrapFileBuffered(f, fileopsEntry)
		}
		return &streamingCachedFile{
			File:   f,
			entry:  fileopsEntry,
			writer: w,
		}, nil
	}

	return r.wrapFileBuffered(f, fileopsEntry)
}

// wrapFileBuffered wraps a file with in-memory buffering for caching.
//
//nolint:gocritic // hugeParam acceptable for Entry value semantics
func (r *Reader) wrapFileBuffered(f *fileops.File, entry fileops.Entry) (fs.File, error) {
	// Check if file is small enough to buffer
	if entry.OriginalSize > uint64(math.MaxInt) {
		// Too large to buffer, skip caching
		return f, nil
	}

	return &bufferedCachedFile{
		File:  f,
		entry: entry,
		cache: r.cache,
		buf:   &bytes.Buffer{},
	}, nil
}

// entryToFileops converts a blob.Entry to a fileops.Entry.
// Both are aliases for blobtype.Entry, so this is a type identity.
//
//nolint:gocritic // hugeParam acceptable for Entry value semantics
func entryToFileops(e blob.Entry) fileops.Entry {
	return e
}

func entryFromViewWithPath(view blob.EntryView, path string) blob.Entry {
	return blob.Entry{
		Path:         path,
		DataOffset:   view.DataOffset(),
		DataSize:     view.DataSize(),
		OriginalSize: view.OriginalSize(),
		Hash:         view.HashBytes(),
		Mode:         view.Mode(),
		UID:          view.UID(),
		GID:          view.GID(),
		ModTime:      view.ModTime(),
		Compression:  view.Compression(),
	}
}
