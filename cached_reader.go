package blob

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
)

// Cache provides content-addressed storage for file contents.
//
// Keys are SHA256 hashes of uncompressed file content. Values are the
// uncompressed content. Because keys are content hashes, cache hits
// are implicitly verified—no additional integrity check is needed.
//
// Implementations should handle their own size limits and eviction policies.
type Cache interface {
	// Get retrieves content by its SHA256 hash.
	// Returns nil, false if the content is not cached.
	Get(hash []byte) ([]byte, bool)

	// Put stores content indexed by its SHA256 hash.
	Put(hash []byte, content []byte) error
}

// StreamingCache extends Cache with streaming write support for large files.
//
// Implementations that support streaming (e.g., disk-based caches) should
// implement this interface to allow caching during Open() without buffering
// entire files in memory.
type StreamingCache interface {
	Cache

	// Writer returns a CacheWriter for streaming content into the cache.
	// The hash is the expected SHA256 of the content being written.
	Writer(hash []byte) (CacheWriter, error)
}

// CacheWriter streams content into the cache.
//
// Content is written via Write calls. After all content is written:
//   - Call Commit if the content hash was verified successfully
//   - Call Discard if verification failed or an error occurred
//
// Implementations should buffer writes to a temporary location and only
// make the content available via Cache.Get after Commit is called.
type CacheWriter interface {
	io.Writer

	// Commit finalizes the cache entry, making it available via Get.
	// Must be called after successful hash verification.
	Commit() error

	// Discard aborts the cache write and cleans up temporary data.
	// Must be called if verification fails or an error occurs.
	Discard() error
}

// CachedReader wraps a Reader with content-addressed caching.
//
// CachedReader implements the same fs.FS interfaces as Reader, but checks
// the cache before fetching from the underlying source and caches content
// after successful reads.
//
// For streaming reads via Open(), caching behavior depends on the cache type:
//   - StreamingCache: content streams to cache without full buffering
//   - Basic Cache: content is buffered in memory then cached on Close
type CachedReader struct {
	*Reader
	cache Cache
}

// Interface compliance.
var (
	_ fs.FS         = (*CachedReader)(nil)
	_ fs.StatFS     = (*CachedReader)(nil)
	_ fs.ReadFileFS = (*CachedReader)(nil)
	_ fs.ReadDirFS  = (*CachedReader)(nil)
)

// NewCachedReader wraps a Reader with caching support.
func NewCachedReader(r *Reader, cache Cache) *CachedReader {
	return &CachedReader{
		Reader: r,
		cache:  cache,
	}
}

// Open implements fs.FS with caching support.
//
// For files, the returned fs.File will cache content after successful
// hash verification on Close. For StreamingCache implementations, content
// streams directly to the cache. For basic Cache implementations, content
// is buffered in memory.
func (cr *CachedReader) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a file
	entry, ok := cr.index.Lookup(name)
	if !ok {
		// Not a file, delegate to base (handles directories)
		return cr.Reader.Open(name)
	}

	// Check cache first
	if content, cached := cr.cache.Get(entry.Hash); cached {
		return &cachedContentFile{
			entry:   entry,
			content: content,
		}, nil
	}

	// Open base file and wrap for caching
	baseFile, err := cr.Reader.Open(name)
	if err != nil {
		return nil, err
	}

	f, ok := baseFile.(*file)
	if !ok {
		// Should not happen for files, but return base file if it does
		return baseFile, nil
	}

	return cr.wrapFileForCaching(f, &entry)
}

// ReadFile implements fs.ReadFileFS with caching support.
//
// ReadFile checks the cache first and returns cached content if available.
// On cache miss, it reads from the source and caches the result.
func (cr *CachedReader) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}

	entry, ok := cr.index.Lookup(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}

	// Check cache first
	if content, cached := cr.cache.Get(entry.Hash); cached {
		return content, nil
	}

	// Read from base reader
	content, err := cr.Reader.ReadFile(name)
	if err != nil {
		return nil, err
	}

	// Cache the content (errors are non-fatal)
	_ = cr.cache.Put(entry.Hash, content) //nolint:errcheck // caching is opportunistic

	return content, nil
}

// Prefetch fetches and caches the specified files.
//
// For adjacent files, Prefetch batches range requests to minimize round trips.
// This is useful for warming the cache with files that will be accessed soon.
func (cr *CachedReader) Prefetch(paths ...string) error {
	// Collect entries for paths that exist and aren't already cached
	entries := make([]Entry, 0, len(paths))
	for _, path := range paths {
		if !fs.ValidPath(path) {
			continue
		}
		entry, ok := cr.index.Lookup(path)
		if !ok {
			continue
		}
		// Skip if already cached
		if _, cached := cr.cache.Get(entry.Hash); cached {
			continue
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil
	}

	// Sort by DataOffset for batching
	slices.SortFunc(entries, func(a, b Entry) int {
		if a.DataOffset < b.DataOffset {
			return -1
		}
		if a.DataOffset > b.DataOffset {
			return 1
		}
		return 0
	})

	return cr.prefetchEntries(entries)
}

// PrefetchDir fetches and caches all files under the given directory prefix.
//
// Because files are sorted by path and stored adjacently, PrefetchDir can
// fetch an entire directory's contents with a single range request, then
// split and cache each file individually.
func (cr *CachedReader) PrefetchDir(prefix string) error {
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

	var entries []Entry //nolint:prealloc // size unknown until iteration
	for entry := range cr.index.EntriesWithPrefix(dirPrefix) {
		// Skip if already cached
		if _, cached := cr.cache.Get(entry.Hash); cached {
			continue
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil
	}

	// Entries are already sorted by path (and thus by offset) from the index
	return cr.prefetchEntries(entries)
}

// prefetchEntries fetches and caches a list of entries, batching adjacent ones.
func (cr *CachedReader) prefetchEntries(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	sourceSize := cr.source.Size()
	for i := range entries {
		if err := validateEntry(&entries[i], sourceSize, cr.maxFileSize); err != nil {
			return err
		}
	}

	groups := groupAdjacentEntries(entries)

	for _, group := range groups {
		if err := cr.prefetchGroup(group); err != nil {
			return err
		}
	}

	return nil
}

// prefetchGroup fetches a contiguous range and caches each entry.
func (cr *CachedReader) prefetchGroup(group rangeGroup) error {
	size := group.end - group.start
	sizeInt, err := sizeToInt(size)
	if err != nil {
		return fmt.Errorf("prefetch: %w", err)
	}
	data := make([]byte, sizeInt)
	n, err := cr.source.ReadAt(data, int64(group.start)) //nolint:gosec // offset fits in int64
	if err != nil && err != io.EOF {
		return fmt.Errorf("prefetch: %w", err)
	}
	if uint64(n) != size { //nolint:gosec // n is always non-negative
		return fmt.Errorf("prefetch: short read (%d of %d bytes)", n, size)
	}

	for i := range group.entries {
		if err := cr.decompressVerifyCache(&group.entries[i], data, group.start); err != nil {
			return err
		}
	}
	return nil
}

// decompressVerifyCache decompresses entry data, verifies its hash, and caches it.
func (cr *CachedReader) decompressVerifyCache(entry *Entry, groupData []byte, groupStart uint64) error {
	localOffset := entry.DataOffset - groupStart
	localEnd := localOffset + entry.DataSize
	if localEnd < localOffset || localEnd > uint64(len(groupData)) {
		return ErrSizeOverflow
	}
	start, err := sizeToInt(localOffset)
	if err != nil {
		return err
	}
	end, err := sizeToInt(localEnd)
	if err != nil {
		return err
	}
	entryData := groupData[start:end]

	content, err := decompress(entryData, entry.Compression, entry.OriginalSize, cr.maxDecoderMemory)
	if err != nil {
		return err
	}

	hash := sha256.Sum256(content)
	if !bytes.Equal(hash[:], entry.Hash) {
		return ErrHashMismatch
	}

	// Cache errors are non-fatal
	_ = cr.cache.Put(entry.Hash, content) //nolint:errcheck // caching is opportunistic
	return nil
}

// wrapFileForCaching wraps a base file with caching support.
func (cr *CachedReader) wrapFileForCaching(f *file, entry *Entry) (fs.File, error) {
	// Check if cache supports streaming
	if sc, ok := cr.cache.(StreamingCache); ok {
		w, err := sc.Writer(entry.Hash)
		if err != nil {
			// Fall back to buffered caching
			return cr.wrapFileBuffered(f, entry)
		}
		return &streamingCachedFile{
			file:   f,
			entry:  *entry,
			writer: w,
		}, nil
	}

	return cr.wrapFileBuffered(f, entry)
}

// wrapFileBuffered wraps a file with in-memory buffering for caching.
func (cr *CachedReader) wrapFileBuffered(f *file, entry *Entry) (fs.File, error) {
	// Check if file is small enough to buffer
	if entry.OriginalSize > uint64(math.MaxInt) {
		// Too large to buffer, skip caching
		return f, nil
	}

	return &bufferedCachedFile{
		file:  f,
		entry: *entry,
		cache: cr.cache,
		buf:   &bytes.Buffer{},
	}, nil
}

// cachedContentFile wraps already-cached content as an fs.File.
type cachedContentFile struct {
	entry   Entry
	content []byte
	offset  int
}

func (f *cachedContentFile) Read(p []byte) (int, error) {
	if f.offset >= len(f.content) {
		return 0, io.EOF
	}
	n := copy(p, f.content[f.offset:])
	f.offset += n
	return n, nil
}

func (f *cachedContentFile) Stat() (fs.FileInfo, error) {
	return &fileInfo{entry: f.entry, name: pathBase(f.entry.Path)}, nil
}

func (f *cachedContentFile) Close() error {
	return nil
}

// streamingCachedFile wraps a file and streams reads to a CacheWriter.
type streamingCachedFile struct {
	*file
	entry   Entry
	writer  CacheWriter
	written bool
	failed  bool
}

func (f *streamingCachedFile) Read(p []byte) (int, error) {
	n, err := f.file.Read(p)

	if n > 0 && !f.failed {
		if _, werr := f.writer.Write(p[:n]); werr != nil {
			// Cache write failed, mark as failed but continue reading
			f.failed = true
		}
		f.written = true
	}

	return n, err
}

func (f *streamingCachedFile) Close() error {
	err := f.file.Close()

	// Handle cache finalization
	switch {
	case f.failed || err != nil:
		_ = f.writer.Discard() //nolint:errcheck // discard is best-effort
	case f.written || f.entry.OriginalSize == 0:
		// Commit on success (or for empty files)
		_ = f.writer.Commit() //nolint:errcheck // caching is opportunistic
	default:
		// Never read anything, discard
		_ = f.writer.Discard() //nolint:errcheck // discard is best-effort
	}

	return err
}

// bufferedCachedFile wraps a file and buffers reads for caching.
type bufferedCachedFile struct {
	*file
	entry Entry
	cache Cache
	buf   *bytes.Buffer
}

func (f *bufferedCachedFile) Read(p []byte) (int, error) {
	n, err := f.file.Read(p)

	if n > 0 && f.buf != nil {
		if _, werr := f.buf.Write(p[:n]); werr != nil {
			// Buffer write failed, disable caching
			f.buf = nil
		}
	}

	return n, err
}

func (f *bufferedCachedFile) Close() error {
	err := f.file.Close()

	// Cache content on success
	if err == nil && f.buf != nil && uint64(f.buf.Len()) == f.entry.OriginalSize { //nolint:gosec // Len() is always non-negative
		_ = f.cache.Put(f.entry.Hash, f.buf.Bytes()) //nolint:errcheck // caching is opportunistic
	}

	return err
}
