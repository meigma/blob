package cache

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"

	"github.com/meigma/blob"
	"github.com/meigma/blob/internal/fileops"
	"github.com/meigma/blob/internal/sizing"
)

const prefetchParallelMinAvgBytes = 64 << 10

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
	entry := view.Entry()

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
	entry := view.Entry()

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
	// Collect entries for paths that exist and aren't already cached
	entries := make([]fileops.Entry, 0, len(paths))
	for _, path := range paths {
		if !fs.ValidPath(path) {
			continue
		}
		view, ok := r.base.Index().LookupView(path)
		if !ok {
			continue
		}
		entry := view.Entry()
		// Skip if already cached
		if _, cached := r.cache.Get(entry.Hash); cached {
			continue
		}
		entries = append(entries, entryToFileops(entry))
	}

	if len(entries) == 0 {
		return nil
	}

	// Sort by DataOffset for batching
	slices.SortFunc(entries, func(a, b fileops.Entry) int {
		if a.DataOffset < b.DataOffset {
			return -1
		}
		if a.DataOffset > b.DataOffset {
			return 1
		}
		return 0
	})

	return r.prefetchEntries(entries)
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

	// Collect all entries under prefix that aren't cached
	var dirPrefix string
	if prefix == "" || prefix == "." {
		dirPrefix = ""
	} else {
		dirPrefix = prefix + "/"
	}

	var entries []fileops.Entry //nolint:prealloc // size unknown until iteration
	for view := range r.base.Index().EntriesWithPrefixView(dirPrefix) {
		entry := view.Entry()
		// Skip if already cached
		if _, cached := r.cache.Get(entry.Hash); cached {
			continue
		}
		entries = append(entries, entryToFileops(entry))
	}

	if len(entries) == 0 {
		return nil
	}

	// Entries are already sorted by path (and thus by offset) from the index
	return r.prefetchEntries(entries)
}

// prefetchEntries fetches and caches a list of entries, batching adjacent ones.
func (r *Reader) prefetchEntries(entries []fileops.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	sourceSize := r.base.Ops().Source().Size()
	maxFileSize := r.base.Ops().MaxFileSize()
	for i := range entries {
		if err := fileops.ValidateAll(&entries[i], sourceSize, maxFileSize); err != nil {
			return err
		}
	}

	groups := groupAdjacentEntries(entries)

	for _, group := range groups {
		if err := r.prefetchGroup(group); err != nil {
			return err
		}
	}

	return nil
}

// prefetchGroup fetches a contiguous range and caches each entry.
func (r *Reader) prefetchGroup(group rangeGroup) error {
	data, err := r.readGroupData(group)
	if err != nil {
		return err
	}
	if len(group.entries) == 0 {
		return nil
	}

	workers := r.prefetchWorkerCount(group.entries)
	if workers < 2 {
		return r.processEntriesSerial(group.entries, data, group.start)
	}
	return r.processEntriesParallel(group.entries, data, group.start, workers)
}

// readGroupData reads the contiguous data for a range group.
func (r *Reader) readGroupData(group rangeGroup) ([]byte, error) {
	size := group.end - group.start
	sizeInt, err := sizing.ToInt(size, blob.ErrSizeOverflow)
	if err != nil {
		return nil, fmt.Errorf("prefetch: %w", err)
	}
	data := make([]byte, sizeInt)
	n, err := r.base.Ops().Source().ReadAt(data, int64(group.start)) //nolint:gosec // offset fits in int64
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("prefetch: %w", err)
	}
	if uint64(n) != size { //nolint:gosec // n is always non-negative
		return nil, fmt.Errorf("prefetch: short read (%d of %d bytes)", n, size)
	}
	return data, nil
}

// processEntriesSerial processes entries sequentially.
func (r *Reader) processEntriesSerial(entries []fileops.Entry, data []byte, groupStart uint64) error {
	for i := range entries {
		if err := r.decompressVerifyCache(&entries[i], data, groupStart); err != nil {
			return err
		}
	}
	return nil
}

// processEntriesParallel processes entries concurrently with the given number of workers.
func (r *Reader) processEntriesParallel(entries []fileops.Entry, data []byte, groupStart uint64, workers int) error {
	var stop atomic.Bool
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for w := range workers {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for i := start; i < len(entries); i += workers {
				if stop.Load() {
					return
				}
				if err := r.decompressVerifyCache(&entries[i], data, groupStart); err != nil {
					if stop.CompareAndSwap(false, true) {
						errCh <- err
					}
					return
				}
			}
		}(w)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// decompressVerifyCache decompresses entry data, verifies its hash, and caches it.
func (r *Reader) decompressVerifyCache(entry *fileops.Entry, groupData []byte, groupStart uint64) error {
	localOffset := entry.DataOffset - groupStart
	localEnd := localOffset + entry.DataSize
	if localEnd < localOffset || localEnd > uint64(len(groupData)) {
		return blob.ErrSizeOverflow
	}
	start, err := sizing.ToInt(localOffset, blob.ErrSizeOverflow)
	if err != nil {
		return err
	}
	end, err := sizing.ToInt(localEnd, blob.ErrSizeOverflow)
	if err != nil {
		return err
	}
	entryData := groupData[start:end]

	if sc, ok := r.cache.(StreamingCache); ok {
		return r.streamDecompressVerifyCache(entry, entryData, sc)
	}
	return r.bufferDecompressVerifyCache(entry, entryData)
}

func (r *Reader) bufferDecompressVerifyCache(entry *fileops.Entry, data []byte) error {
	content, err := r.decompress(data, entry.Compression, entry.OriginalSize)
	if err != nil {
		return err
	}

	hash := sha256.Sum256(content)
	if !bytes.Equal(hash[:], entry.Hash) {
		return blob.ErrHashMismatch
	}

	// Cache errors are non-fatal.
	_ = r.cache.Put(entry.Hash, content) //nolint:errcheck // caching is opportunistic
	return nil
}

func (r *Reader) streamDecompressVerifyCache(entry *fileops.Entry, data []byte, sc StreamingCache) error {
	w, err := sc.Writer(entry.Hash)
	if err != nil {
		return r.bufferDecompressVerifyCache(entry, data)
	}

	reader, closeFn, err := r.newEntryReader(entry, data)
	if err != nil {
		_ = w.Discard() //nolint:errcheck // best-effort cleanup in error path
		return err
	}
	defer closeFn()

	hasher := sha256.New()
	tee := io.TeeReader(reader, hasher)

	expected, err := sizing.ToInt64(entry.OriginalSize, blob.ErrSizeOverflow)
	if err != nil {
		_ = w.Discard() //nolint:errcheck // best-effort cleanup in error path
		return err
	}
	if _, err := io.CopyN(w, tee, expected); err != nil {
		_ = w.Discard() //nolint:errcheck // best-effort cleanup in error path
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("%w: unexpected EOF", blob.ErrDecompression)
		}
		return err
	}
	if err := fileops.EnsureNoExtra(tee); err != nil {
		_ = w.Discard() //nolint:errcheck // best-effort cleanup in error path
		return err
	}

	sum := hasher.Sum(nil)
	if !bytes.Equal(sum, entry.Hash) {
		_ = w.Discard() //nolint:errcheck // best-effort cleanup in error path
		return blob.ErrHashMismatch
	}
	return w.Commit()
}

// decompress decompresses data according to the compression algorithm.
func (r *Reader) decompress(data []byte, comp fileops.Compression, expectedSize uint64) ([]byte, error) {
	switch comp {
	case fileops.CompressionNone:
		if uint64(len(data)) != expectedSize {
			return nil, fmt.Errorf("%w: size mismatch", blob.ErrDecompression)
		}
		return data, nil
	case fileops.CompressionZstd:
		contentSize, err := sizing.ToInt(expectedSize, blob.ErrSizeOverflow)
		if err != nil {
			return nil, err
		}
		dec, closeFn, err := r.base.Ops().Pool().Get(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", blob.ErrDecompression, err)
		}
		defer closeFn()

		content := make([]byte, contentSize)
		if _, err := io.ReadFull(dec, content); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("%w: unexpected EOF", blob.ErrDecompression)
			}
			return nil, fmt.Errorf("%w: %v", blob.ErrDecompression, err)
		}
		if err := fileops.EnsureNoExtra(dec); err != nil {
			return nil, err
		}
		return content, nil
	default:
		return nil, fmt.Errorf("unknown compression algorithm: %d", comp)
	}
}

func (r *Reader) newEntryReader(entry *fileops.Entry, data []byte) (io.Reader, func(), error) {
	switch entry.Compression {
	case fileops.CompressionNone:
		return bytes.NewReader(data), func() {}, nil
	case fileops.CompressionZstd:
		dec, closeFn, err := r.base.Ops().Pool().Get(bytes.NewReader(data))
		if err != nil {
			return nil, func() {}, fmt.Errorf("%w: %v", blob.ErrDecompression, err)
		}
		return dec, closeFn, nil
	default:
		return nil, func() {}, fmt.Errorf("unknown compression algorithm: %d", entry.Compression)
	}
}

func (r *Reader) prefetchWorkerCount(entries []fileops.Entry) int {
	if len(entries) < 2 {
		return 1
	}
	if r.prefetchWorkers < 0 {
		return 1
	}

	workers := r.prefetchWorkers
	if workers == 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers < 2 {
			return 1
		}
		if _, ok := r.cache.(StreamingCache); ok {
			var total uint64
			for i := range entries {
				next, ok := sizing.AddUint64(total, entries[i].OriginalSize)
				if !ok {
					total = ^uint64(0)
					break
				}
				total = next
			}
			if total/uint64(len(entries)) < prefetchParallelMinAvgBytes {
				return 1
			}
		}
	}

	if workers > len(entries) {
		workers = len(entries)
	}
	if workers < 2 {
		return 1
	}
	return workers
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
