---
sidebar_position: 3
---

# Caching

How to use content-addressed caching for improved performance.

## When to Use Caching

Caching improves performance in these scenarios:

- **Repeated access**: Reading the same files multiple times (e.g., rebuilding a project)
- **Shared content**: Multiple archives containing identical files (automatic deduplication via content hashing)
- **Remote archives**: Avoiding repeated network round trips to OCI registries

The cache uses SHA256 hashes of uncompressed file content as keys. This provides:
- Automatic deduplication across archives
- Implicit integrity verification on cache hits
- Efficient storage of shared dependencies

## Disk Cache

To create a disk-backed cache:

```go
import (
	"github.com/meigma/blob/cache"
	"github.com/meigma/blob/cache/disk"
)

diskCache, err := disk.New("/path/to/cache")
if err != nil {
	return err
}
```

The disk cache automatically creates the directory if it does not exist and uses a sharded directory structure to avoid filesystem performance issues with many files.

### Cache Options

Configure the disk cache with options:

```go
diskCache, err := disk.New("/path/to/cache",
	disk.WithShardPrefixLen(3),   // Use 3-character prefix for sharding (default: 2)
	disk.WithDirPerm(0o750),      // Set directory permissions (default: 0o700)
)
```

The shard prefix length determines directory distribution:
- `2` (default): Creates 256 subdirectories (00-ff)
- `3`: Creates 4096 subdirectories (000-fff)
- `0`: Disables sharding (all files in one directory)

## Wrapping a Blob with Caching

To add caching to an existing blob:

```go
import (
	"github.com/meigma/blob"
	"github.com/meigma/blob/cache"
	"github.com/meigma/blob/cache/disk"
)

func openCachedArchive(indexData []byte, source blob.ByteSource) (*cache.Blob, error) {
	// Create the base blob
	base, err := blob.New(indexData, source)
	if err != nil {
		return nil, err
	}

	// Create disk cache
	diskCache, err := disk.New("/var/cache/blob")
	if err != nil {
		return nil, err
	}

	// Wrap with caching
	return cache.New(base, diskCache), nil
}
```

### Using BlobFile with Caching

When using `OpenFile`, extract the embedded `*Blob` for caching:

```go
blobFile, err := blob.OpenFile("index.blob", "data.blob")
if err != nil {
	return nil, err
}
// Note: caller is responsible for closing blobFile when done

diskCache, err := disk.New("/var/cache/blob")
if err != nil {
	blobFile.Close()
	return nil, err
}

// Wrap the embedded Blob with caching
cached := cache.New(blobFile.Blob, diskCache)
```

The cached blob implements the same `fs.FS` interfaces as the base blob, so you can use it as a drop-in replacement.

## Reading Files

The cached blob automatically handles cache lookups:

```go
// First read: fetches from source, caches result
content, err := cachedBlob.ReadFile("lib/utils.go")

// Second read: returns from cache, no network request
content, err = cachedBlob.ReadFile("lib/utils.go")
```

For streaming reads via `Open()`, behavior depends on the cache type:
- **Disk cache (StreamingCache)**: Content streams directly to cache during read
- **Basic cache**: Content is buffered in memory, then cached on Close

## Prefetching

To warm the cache with files you will access soon, use prefetch:

```go
// Prefetch specific files
err := cachedBlob.Prefetch("go.mod", "go.sum", "main.go")

// Prefetch an entire directory
err = cachedBlob.PrefetchDir("pkg")
```

Prefetching is especially useful for remote archives because:
- Adjacent files are fetched with batched range requests
- Content is cached for subsequent access
- You can prefetch during idle time

### Prefetch Concurrency

By default, prefetch runs serially. To parallelize:

```go
cachedBlob := cache.New(base, diskCache,
	cache.WithPrefetchConcurrency(4), // Use 4 workers
)
```

## Custom Cache Implementations

To implement a custom cache, satisfy the `cache.Cache` interface:

```go
type Cache interface {
	// Get retrieves content by its SHA256 hash.
	// Returns nil, false if the content is not cached.
	Get(hash []byte) ([]byte, bool)

	// Put stores content indexed by its SHA256 hash.
	Put(hash []byte, content []byte) error
}
```

Example in-memory cache:

```go
type MemoryCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{data: make(map[string][]byte)}
}

func (c *MemoryCache) Get(hash []byte) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	content, ok := c.data[string(hash)]
	return content, ok
}

func (c *MemoryCache) Put(hash, content []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[string(hash)] = content
	return nil
}
```

### Streaming Cache Interface

For large files, implement `cache.StreamingCache` to avoid buffering entire files in memory:

```go
type StreamingCache interface {
	Cache

	// Writer returns a Writer for streaming content into the cache.
	// The hash is the expected SHA256 of the content being written.
	Writer(hash []byte) (Writer, error)
}

type Writer interface {
	io.Writer

	// Commit finalizes the cache entry after successful verification.
	Commit() error

	// Discard aborts the write and cleans up temporary data.
	Discard() error
}
```

The disk cache implements this interface, writing to a temporary file and atomically renaming on commit.

## Complete Example

A complete setup with disk caching and prefetch:

```go
func setupCachedArchive(indexData []byte, dataURL string) (*cache.Blob, error) {
	// Create HTTP source
	source, err := http.NewSource(dataURL,
		http.WithHeader("Authorization", "Bearer "+token),
	)
	if err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}

	// Create base blob
	base, err := blob.New(indexData, source)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}

	// Create disk cache in user cache directory
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = "/tmp"
	}
	diskCache, err := disk.New(filepath.Join(cacheDir, "blob"))
	if err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}

	// Wrap with caching
	cached := cache.New(base, diskCache,
		cache.WithPrefetchConcurrency(4),
	)

	// Prefetch commonly accessed directories
	if err := cached.PrefetchDir("src"); err != nil {
		// Non-fatal: prefetch is opportunistic
		log.Printf("prefetch warning: %v", err)
	}

	return cached, nil
}
```

## See Also

- [Working with Remote Archives](remote-archives) - Set up HTTP sources
- [Performance Tuning](performance-tuning) - Optimize prefetch and read concurrency
