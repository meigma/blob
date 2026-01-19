---
sidebar_position: 4
---

# Content Caching

How to use content-addressed caching for file deduplication.

Content caching stores file contents by their SHA256 hash, enabling deduplication across archives and avoiding repeated network transfers.

## Relationship to OCI Client Caches

Blob supports two levels of caching:

| Cache Level | Purpose | What It Caches |
|-------------|---------|----------------|
| **OCI Client Caches** | Minimize registry API calls | Refs, manifests, index blobs |
| **Content Cache** | Deduplicate file content | Actual file bytes by hash |

Configure OCI client caches via `client.WithRefCache()`, `client.WithManifestCache()`, and `client.WithIndexCache()`. See [OCI Client Caching](oci-client-caching) for details.

Content caching (this guide) operates at a different level: it caches the actual file contents after they're fetched from the data blob.

## When to Use Content Caching

Content caching improves performance in these scenarios:

- **Repeated access**: Reading the same files multiple times (e.g., rebuilding a project)
- **Shared content**: Multiple archives containing identical files (automatic deduplication via content hashing)
- **Remote archives**: Avoiding repeated network round trips for file data

The cache uses SHA256 hashes of uncompressed file content as keys. This provides:
- Automatic deduplication across archives
- Implicit integrity verification on cache hits
- Efficient storage of shared dependencies

## Disk Cache

To create a disk-backed cache:

```go
import (
	"github.com/meigma/blob/core/cache"
	"github.com/meigma/blob/core/cache/disk"
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

## Managing Cache Size

Both disk caches (content cache and block cache) support size limits and automatic pruning.

### Setting Size Limits

Specify a maximum cache size when creating the cache:

```go
// Content cache with 1 GB limit
diskCache, err := disk.New("/path/to/cache",
    disk.WithMaxBytes(1 << 30),  // 1 GB
)

// Block cache with 256 MB limit
blockCache, err := disk.NewBlockCache("/path/to/blocks",
    disk.WithBlockMaxBytes(256 << 20),  // 256 MB
)
```

When the cache exceeds its limit, it automatically prunes old entries before adding new ones.

### How Pruning Works

Pruning removes entries by modification time (LRU-style eviction):

1. Entries are sorted by modification time (oldest first)
2. Oldest entries are removed until the cache is under the target size
3. The cache tracks its size in memory for fast capacity checks

### Manual Pruning

To manually prune a cache to a specific size:

```go
// Prune to 100 MB
freed, err := diskCache.Prune(100 << 20)
if err != nil {
    return err
}
fmt.Printf("Freed %d bytes\n", freed)
```

### Monitoring Cache Size

Check the current cache size:

```go
// Get configured limit (0 = unlimited)
maxBytes := diskCache.MaxBytes()

// Get current size
currentBytes := diskCache.SizeBytes()

fmt.Printf("Cache: %d / %d bytes (%.1f%%)\n",
    currentBytes, maxBytes,
    float64(currentBytes)/float64(maxBytes)*100,
)
```

### Sizing Guidelines

| Use Case | Recommended Size | Rationale |
|----------|-----------------|-----------|
| Development workstation | 256 MB - 1 GB | Balance performance with disk usage |
| CI/CD ephemeral | 0 (unlimited) | Disk is reclaimed after job |
| Production server | 2-10 GB | Based on working set size |
| Memory-constrained | 64-128 MB | Minimum useful size |

The optimal size depends on your access patterns. Monitor cache hit rates and adjust accordingly.

## Enabling Content Caching

Content caching is enabled by passing a cache to `blob.New()` or `client.Pull()`:

```go
import (
	"github.com/meigma/blob/core"
	"github.com/meigma/blob/core/cache/disk"
)

func openCachedArchive(indexData []byte, source blob.ByteSource) (*blob.Blob, error) {
	// Create disk cache
	diskCache, err := disk.New("/var/cache/blob")
	if err != nil {
		return nil, err
	}

	// Create blob with caching enabled
	return blob.New(indexData, source, blob.WithCache(diskCache))
}
```

### With OCI Client

When pulling from an OCI registry, pass the cache as a blob option:

```go
import (
	"context"

	"github.com/meigma/blob/core"
	"github.com/meigma/blob/core/cache/disk"
	"github.com/meigma/blob/client"
)

func pullWithCache(c *client.Client, ref string) (*blob.Blob, error) {
	// Create disk cache
	diskCache, err := disk.New("/var/cache/blob")
	if err != nil {
		return nil, err
	}

	// Pull with content caching enabled
	return c.Pull(context.Background(), ref,
		client.WithBlobOptions(blob.WithCache(diskCache)),
	)
}
```

The blob returned by `Pull` will use the cache for all file reads.

## Reading Files

The cached blob automatically handles cache lookups:

```go
// First read: fetches from source, caches result
content, err := archive.ReadFile("lib/utils.go")

// Second read: returns from cache, no network request
content, err = archive.ReadFile("lib/utils.go")
```

Concurrent reads for the same content are deduplicated using singleflight, preventing redundant network requests when multiple goroutines request the same file simultaneously.

For streaming reads via `Open()`, content is cached when the file is read to completion or closed.

## Custom Cache Implementations

To implement a custom cache, satisfy the `cache.Cache` interface:

```go
import "io/fs"

type Cache interface {
	// Get returns an fs.File for reading cached content.
	// Returns nil, false if content is not cached.
	Get(hash []byte) (fs.File, bool)

	// Put stores content by reading from the provided fs.File.
	// The cache reads the file to completion; caller still owns/closes the file.
	Put(hash []byte, f fs.File) error

	// Delete removes cached content for the given hash.
	Delete(hash []byte) error

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes cached entries until the cache is at or below targetBytes.
	Prune(targetBytes int64) (int64, error)
}
```

The disk cache (`github.com/meigma/blob/core/cache/disk`) implements this interface and handles:
- Atomic writes using temporary files and renames
- Sharded directory structure for filesystem performance
- Automatic size tracking and pruning

## Complete Example

A complete setup with OCI client and content caching:

```go
import (
	"context"
	"os"
	"path/filepath"

	"github.com/meigma/blob/core"
	"github.com/meigma/blob/core/cache/disk"
	"github.com/meigma/blob/client"
	clientdisk "github.com/meigma/blob/client/cache/disk"
)

func setupCachedArchive(ref string) (*blob.Blob, error) {
	ctx := context.Background()

	// Create OCI client caches
	refCache, _ := clientdisk.NewRefCache("/var/cache/blob/refs")
	manifestCache, _ := clientdisk.NewManifestCache("/var/cache/blob/manifests")
	indexCache, _ := clientdisk.NewIndexCache("/var/cache/blob/indexes")

	c := client.New(
		client.WithDockerConfig(),
		client.WithRefCache(refCache),
		client.WithManifestCache(manifestCache),
		client.WithIndexCache(indexCache),
	)

	// Create content cache in user cache directory
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = "/tmp"
	}
	contentCache, err := disk.New(filepath.Join(cacheDir, "blob", "content"))
	if err != nil {
		return nil, err
	}

	// Pull with content caching enabled
	archive, err := c.Pull(ctx, ref,
		client.WithBlobOptions(blob.WithCache(contentCache)),
	)
	if err != nil {
		return nil, err
	}

	return archive, nil
}
```

## See Also

- [OCI Client Caching](oci-client-caching) - Configure OCI client caches
- [Block Caching](block-caching) - Block-level caching for random access
- [OCI Client](oci-client) - Push and pull archives
- [Performance Tuning](performance-tuning) - Optimize prefetch and read concurrency
