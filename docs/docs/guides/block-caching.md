---
sidebar_position: 5
---

# Block Caching

How to use block-level caching to optimize random access reads from OCI registries.

Block caching operates at the data blob level, caching fixed-size blocks of raw data to reduce HTTP range requests to the registry.

## When to Use Block Caching

Block caching improves performance in these scenarios:

- **Scattered random reads**: Accessing individual files spread across the archive
- **OCI registries with latency**: Each range request has network overhead
- **Partial file reads**: Reading portions of large files multiple times

Block caching is NOT recommended for:

- Sequential directory extraction (use CopyDir without caching)
- Single-pass full file reads (block overhead exceeds benefit)
- Local file sources (no network latency to hide)

### How It Differs from Content Caching

| Aspect | Content Cache | Block Cache |
|--------|---------------|-------------|
| Cache key | SHA256 of file content | SHA256(sourceID + blockSize + blockIndex) |
| Granularity | Whole files | Fixed-size blocks (default 64KB) |
| Deduplication | Across archives | Within single source |
| Best for | Repeated access, shared content | Random access, remote sources |

Use content caching when you read the same files repeatedly or share content across archives. Use block caching when you need fast random access to remote data.

## Creating a Block Cache

To create a disk-backed block cache:

```go
import (
    "github.com/meigma/blob/cache"
    "github.com/meigma/blob/cache/disk"
)

blockCache, err := disk.NewBlockCache("/path/to/cache")
if err != nil {
    return err
}
```

### Block Cache Options

Configure the block cache with options:

```go
blockCache, err := disk.NewBlockCache("/path/to/cache",
    disk.WithBlockMaxBytes(512 << 20),    // 512 MB cache limit
    disk.WithBlockShardPrefixLen(3),      // 3-character sharding
    disk.WithBlockDirPerm(0o750),         // Directory permissions
)
```

## Wrapping a Source with Block Caching

To add block caching to an HTTP source:

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/http"
    "github.com/meigma/blob/cache"
    "github.com/meigma/blob/cache/disk"
)

// Create HTTP source
source, err := http.NewSource(dataURL)
if err != nil {
    return err
}

// Create block cache
blockCache, err := disk.NewBlockCache("/var/cache/blob-blocks")
if err != nil {
    return err
}

// Wrap source with caching
cachedSource, err := blockCache.Wrap(source)
if err != nil {
    return err
}

// Use cachedSource with blob.New
archive, err := blob.New(indexData, cachedSource)
```

### Wrap Options

Configure per-source wrapping behavior:

```go
cachedSource, err := blockCache.Wrap(source,
    cache.WithBlockSize(128 << 10),    // 128 KB blocks
    cache.WithMaxBlocksPerRead(8),     // Bypass for reads spanning > 8 blocks
)
```

## Block Size Selection

The block size affects cache efficiency:

| Block Size | Best For | Trade-offs |
|-----------|----------|------------|
| 16 KB | Small files, fine-grained access | More metadata overhead |
| 64 KB (default) | Balanced workloads | Good general choice |
| 256 KB | Large sequential reads | Wasted space on partial reads |

Choose smaller blocks when files are small or access is fine-grained. Choose larger blocks when reads tend to be sequential within files.

### Bypass for Large Reads

The `MaxBlocksPerRead` option bypasses caching when a single ReadAt spans too many blocks. This prevents sequential reads from polluting the block cache:

```go
// Default: 4 blocks. A 256 KB read with 64 KB blocks = 4 blocks = cached
// A 1 MB read with 64 KB blocks = 16 blocks = bypassed

cachedSource, err := blockCache.Wrap(source,
    cache.WithMaxBlocksPerRead(4),  // Bypass reads spanning > 4 blocks
)
```

Set to 0 to disable the limit and cache all reads.

## SourceID Requirements

Block cache keys depend on a stable source identifier. The HTTP source automatically generates one from URL, ETag, and Last-Modified headers:

```go
// Automatic: url:https://example.com/data|etag:"abc123"
source, err := http.NewSource(dataURL)

// Override if needed
source, err := http.NewSource(dataURL,
    http.WithSourceID("my-custom-identifier"),
)
```

For custom ByteSource implementations, implement the `SourceID()` method:

```go
type MySource struct {
    // ...
}

func (s *MySource) SourceID() string {
    return fmt.Sprintf("mysource:%s:%d", s.identifier, s.version)
}
```

The SourceID must be stable for the same content and change when content changes. Using content hashes or version identifiers is recommended.

## Concurrent Access

The block cache uses singleflight to deduplicate concurrent fetches. Multiple goroutines requesting the same block share a single network request:

```go
// These run in parallel but only one network request occurs
go archive.ReadFile("large-file.bin") // Needs block 42
go archive.ReadFile("large-file.bin") // Also needs block 42 - shares fetch
```

## Complete Example

Block caching integrates with the OCI client's data source. When using the OCI client, the pulled archive already uses HTTP range requests to the registry. To add block caching, wrap the underlying source:

```go
import (
	"os"
	"path/filepath"

	"github.com/meigma/blob"
	"github.com/meigma/blob/cache"
	"github.com/meigma/blob/cache/disk"
	"github.com/meigma/blob/http"
)

func setupBlockCachedArchive(indexData []byte, dataURL, token string) (*blob.Blob, error) {
	// Create HTTP source with authentication
	source, err := http.NewSource(dataURL,
		http.WithHeader("Authorization", "Bearer "+token),
	)
	if err != nil {
		return nil, err
	}

	// Create block cache with size limit
	cacheDir, _ := os.UserCacheDir()
	blockCache, err := disk.NewBlockCache(
		filepath.Join(cacheDir, "blob-blocks"),
		disk.WithBlockMaxBytes(256<<20), // 256 MB limit
	)
	if err != nil {
		return nil, err
	}

	// Wrap source with block caching
	cachedSource, err := blockCache.Wrap(source,
		cache.WithBlockSize(64<<10),  // 64 KB blocks
		cache.WithMaxBlocksPerRead(4), // Bypass large sequential reads
	)
	if err != nil {
		return nil, err
	}

	return blob.New(indexData, cachedSource)
}
```

For most use cases with OCI registries, the OCI client caches (RefCache, ManifestCache, IndexCache) combined with content caching provide sufficient performance. Block caching is useful when you need fine-grained control over data blob access patterns.

## See Also

- [OCI Client](oci-client) - Push and pull archives
- [OCI Client Caching](oci-client-caching) - Configure client caches
- [Caching](caching) - Content-addressed caching for deduplication
- [Performance Tuning](performance-tuning) - Optimize read patterns
