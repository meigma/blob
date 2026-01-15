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

// Blob wraps a blob.Blob with content-addressed caching.
//
// Blob implements the same fs.FS interfaces as blob.Blob, but checks
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
// Blob uses singleflight to deduplicate concurrent ReadFile calls
// for the same content, preventing redundant network requests during
// cache miss storms.
type Blob struct {
	base            *blob.Blob
	cache           Cache
	prefetchWorkers int
	fetchGroup      singleflight.Group
}

// Interface compliance.
var (
	_ fs.FS         = (*Blob)(nil)
	_ fs.StatFS     = (*Blob)(nil)
	_ fs.ReadFileFS = (*Blob)(nil)
	_ fs.ReadDirFS  = (*Blob)(nil)
)

// Option configures a Blob.
type Option func(*Blob)

// WithPrefetchConcurrency sets the number of workers used for Prefetch/PrefetchDir.
// Values < 0 force serial execution. Zero uses a size-based heuristic.
// Values > 0 force a fixed worker count.
func WithPrefetchConcurrency(workers int) Option {
	return func(b *Blob) {
		b.prefetchWorkers = workers
	}
}

// New wraps a blob.Blob with caching support.
func New(base *blob.Blob, cache Cache, opts ...Option) *Blob {
	b := &Blob{
		base:  base,
		cache: cache,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(b)
	}
	return b
}

// Open implements fs.FS with caching support.
//
// For files, the returned fs.File will cache content after successful
// hash verification on Close. For StreamingCache implementations, content
// streams directly to the cache. For basic Cache implementations, content
// is buffered in memory.
func (b *Blob) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	view, ok := b.base.Entry(name)
	if !ok {
		// Not a file, delegate to base (handles directories)
		return b.base.Open(name)
	}
	entry := blob.EntryFromViewWithPath(view, name)

	// Check cache first
	if content, cached := b.cache.Get(entry.Hash); cached {
		return &cachedContentFile{
			entry:   entryToFileops(entry),
			content: content,
		}, nil
	}

	// Open base file and wrap for caching
	baseFile, err := b.base.Open(name)
	if err != nil {
		return nil, err
	}

	f, ok := baseFile.(*fileops.File)
	if !ok {
		// Should not happen for files, but return base file if it does
		return baseFile, nil
	}

	return b.wrapFileForCaching(f, entry)
}

// Stat implements fs.StatFS.
func (b *Blob) Stat(name string) (fs.FileInfo, error) {
	return b.base.Stat(name)
}

// ReadFile implements fs.ReadFileFS with caching support.
//
// ReadFile checks the cache first and returns cached content if available.
// On cache miss, it reads from the source and caches the result.
//
// Concurrent calls for the same content are deduplicated using singleflight,
// so only one network request is made even if multiple goroutines request
// the same file simultaneously.
func (b *Blob) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	view, ok := b.base.Entry(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	entry := blob.EntryFromViewWithPath(view, name)

	// Check cache first (fast path, avoids singleflight overhead)
	if content, cached := b.cache.Get(entry.Hash); cached {
		return content, nil
	}

	// Use content hash as singleflight key. All concurrent callers requesting
	// the same content (even via different paths) share a single fetch.
	key := string(entry.Hash)

	result, err, _ := b.fetchGroup.Do(key, func() (any, error) {
		// Double-check cache: another goroutine may have just cached this
		// content between our cache check and acquiring the singleflight lock.
		if content, cached := b.cache.Get(entry.Hash); cached {
			return content, nil
		}

		// Fetch from source
		content, err := b.base.ReadFile(name)
		if err != nil {
			return nil, err
		}

		// Cache the content (errors are non-fatal)
		_ = b.cache.Put(entry.Hash, content) //nolint:errcheck // caching is opportunistic

		return content, nil
	})

	if err != nil {
		return nil, err
	}

	content, _ := result.([]byte) //nolint:errcheck // type assertion always succeeds when err is nil
	return content, nil
}

// ReadDir implements fs.ReadDirFS.
func (b *Blob) ReadDir(name string) ([]fs.DirEntry, error) {
	return b.base.ReadDir(name)
}

// Prefetch fetches and caches the specified files.
//
// For adjacent files, Prefetch batches range requests to minimize round trips.
// This is useful for warming the cache with files that will be accessed soon.
func (b *Blob) Prefetch(paths ...string) error {
	return b.prefetchEntries(b.collectEntriesForPaths(paths))
}

// PrefetchDir fetches and caches all files under the given directory prefix.
//
// Because files are sorted by path and stored adjacently, PrefetchDir can
// fetch an entire directory's contents with a single range request, then
// split and cache each file individually.
func (b *Blob) PrefetchDir(prefix string) error {
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

	return b.prefetchEntries(b.collectEntriesWithPrefix(dirPrefix))
}

func (b *Blob) collectEntriesForPaths(paths []string) []*batch.Entry {
	entries := make([]batch.Entry, 0, len(paths))
	for _, path := range paths {
		if !fs.ValidPath(path) {
			continue
		}
		view, ok := b.base.Entry(path)
		if !ok {
			continue
		}
		entry := blob.EntryFromViewWithPath(view, path)
		entries = append(entries, entry)
	}
	return entryPointers(entries)
}

func (b *Blob) collectEntriesWithPrefix(prefix string) []*batch.Entry {
	var entries []batch.Entry //nolint:prealloc // size unknown until iteration
	for view := range b.base.EntriesWithPrefix(prefix) {
		entry := blob.EntryFromViewWithPath(view, "")
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
func (b *Blob) prefetchEntries(entries []*batch.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	workers := b.prefetchWorkers
	if workers == 0 {
		if _, ok := b.cache.(StreamingCache); !ok {
			workers = runtime.GOMAXPROCS(0)
		}
	}

	var procOpts []batch.ProcessorOption
	if workers != 0 {
		procOpts = append(procOpts, batch.WithWorkers(workers))
	}
	proc := batch.NewProcessor(b.base.Ops().Source(), b.base.Ops().Pool(), b.base.Ops().MaxFileSize(), procOpts...)

	var sink batch.Sink = &cacheSink{cache: b.cache}
	if _, ok := b.cache.(StreamingCache); !ok {
		sink = &nonStreamingCacheSink{cacheSink: &cacheSink{cache: b.cache}}
	}
	return proc.Process(entries, sink)
}

// wrapFileForCaching wraps a base file with caching support.
//
//nolint:gocritic // hugeParam acceptable for Entry value semantics
func (b *Blob) wrapFileForCaching(f *fileops.File, entry blob.Entry) (fs.File, error) {
	fileopsEntry := entryToFileops(entry)

	// Check if cache supports streaming
	if sc, ok := b.cache.(StreamingCache); ok {
		w, err := sc.Writer(entry.Hash)
		if err != nil {
			// Fall back to buffered caching
			return b.wrapFileBuffered(f, fileopsEntry)
		}
		return &streamingCachedFile{
			File:   f,
			entry:  fileopsEntry,
			writer: w,
		}, nil
	}

	return b.wrapFileBuffered(f, fileopsEntry)
}

// wrapFileBuffered wraps a file with in-memory buffering for caching.
//
//nolint:gocritic // hugeParam acceptable for Entry value semantics
func (b *Blob) wrapFileBuffered(f *fileops.File, entry fileops.Entry) (fs.File, error) {
	// Check if file is small enough to buffer
	if entry.OriginalSize > uint64(math.MaxInt) {
		// Too large to buffer, skip caching
		return f, nil
	}

	return &bufferedCachedFile{
		File:  f,
		entry: entry,
		cache: b.cache,
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
