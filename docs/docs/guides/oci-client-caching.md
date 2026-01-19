---
sidebar_position: 3
---

# OCI Client Caching

How to configure client-level caches for OCI registry operations.

The OCI client supports three tiers of caching that work together to minimize network requests to the registry. These caches store metadata, not file content.

## Cache Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     OCI Client Caches                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐    ┌─────────────────┐    ┌─────────────┐  │
│  │  RefCache   │───▶│  ManifestCache  │───▶│  IndexCache │  │
│  │             │    │                 │    │             │  │
│  │ tag→digest  │    │ digest→manifest │    │ digest→blob │  │
│  └─────────────┘    └─────────────────┘    └─────────────┘  │
│                                                             │
│  Avoids tag        Avoids manifest       Avoids index       │
│  resolution        fetch requests        blob downloads     │
│  HEAD requests                                              │
└─────────────────────────────────────────────────────────────┘
```

## RefCache: Tag to Digest Mapping

RefCache stores the mapping from mutable references (tags) to immutable digests. This avoids redundant HEAD requests to resolve tags.

### Setup

```go
import "github.com/meigma/blob/client/cache/disk"

refCache, err := disk.NewRefCache("/var/cache/blob/refs")
if err != nil {
    return err
}
```

### TTL for Mutable Tags

Tags like `latest` can change over time. Use TTL to automatically expire cached entries:

```go
refCache, err := disk.NewRefCache("/var/cache/blob/refs",
    disk.WithRefCacheTTL(5 * time.Minute),
)
```

With TTL enabled:
- Cached entries older than the TTL are treated as cache misses
- Expired entries are automatically deleted on access
- Use `0` to disable TTL (entries never expire based on age)

### Size Limits

```go
refCache, err := disk.NewRefCache("/var/cache/blob/refs",
    disk.WithMaxBytes(10 << 20),  // 10 MB limit
)
```

## ManifestCache: Digest to Manifest Mapping

ManifestCache stores parsed manifests by their content digest. Since digests are content-addressed, cached manifests never become stale.

### Setup

```go
manifestCache, err := disk.NewManifestCache("/var/cache/blob/manifests")
if err != nil {
    return err
}
```

### Integrity Validation

The manifest cache automatically validates cached entries against their digest on read. Corrupted entries are deleted automatically:

```go
// On cache hit, the cache:
// 1. Reads the cached manifest bytes
// 2. Computes SHA256 of the bytes
// 3. Compares against the expected digest
// 4. Returns the manifest only if they match
// 5. Deletes the entry if they don't match
```

### Size Limits

```go
manifestCache, err := disk.NewManifestCache("/var/cache/blob/manifests",
    disk.WithMaxBytes(50 << 20),  // 50 MB limit
)
```

## IndexCache: Digest to Index Blob Mapping

IndexCache stores the raw index blob bytes by their content digest. Like ManifestCache, these are content-addressed and never become stale.

### Setup

```go
indexCache, err := disk.NewIndexCache("/var/cache/blob/indexes")
if err != nil {
    return err
}
```

### Integrity Validation

The index cache validates cached entries against their digest on read:

```go
// On cache hit, the cache:
// 1. Reads the cached index bytes
// 2. Computes SHA256 of the bytes
// 3. Compares against the expected digest
// 4. Returns the bytes only if they match
// 5. Deletes the entry if they don't match
```

### Size Limits

```go
indexCache, err := disk.NewIndexCache("/var/cache/blob/indexes",
    disk.WithMaxBytes(100 << 20),  // 100 MB limit
)
```

## Combining All Three Caches

For best performance, configure all three caches:

```go
import (
    "time"

    "github.com/meigma/blob/client"
    "github.com/meigma/blob/client/cache/disk"
)

func createCachedClient() (*client.Client, error) {
    // RefCache with TTL for mutable tags
    refCache, err := disk.NewRefCache("/var/cache/blob/refs",
        disk.WithRefCacheTTL(5 * time.Minute),
        disk.WithMaxBytes(10 << 20),
    )
    if err != nil {
        return nil, err
    }

    // ManifestCache (no TTL needed - content-addressed)
    manifestCache, err := disk.NewManifestCache("/var/cache/blob/manifests",
        disk.WithMaxBytes(50 << 20),
    )
    if err != nil {
        return nil, err
    }

    // IndexCache (no TTL needed - content-addressed)
    indexCache, err := disk.NewIndexCache("/var/cache/blob/indexes",
        disk.WithMaxBytes(100 << 20),
    )
    if err != nil {
        return nil, err
    }

    return client.New(
        client.WithDockerConfig(),
        client.WithRefCache(refCache),
        client.WithManifestCache(manifestCache),
        client.WithIndexCache(indexCache),
    ), nil
}
```

## Cache Options Reference

All disk caches share these common options:

| Option | Description | Default |
|--------|-------------|---------|
| `WithShardPrefixLen(n)` | Number of hex characters used for directory sharding. Use 0 to disable. | 2 |
| `WithDirPerm(mode)` | Directory permissions for cache directories. | 0700 |
| `WithMaxBytes(n)` | Maximum cache size in bytes. 0 disables the limit. | 0 (unlimited) |

RefCache has an additional option:

| Option | Description | Default |
|--------|-------------|---------|
| `WithRefCacheTTL(ttl)` | Time-to-live for cached entries. 0 disables TTL. | 0 (no expiration) |

## Cache Size Management

### Automatic Pruning

When a cache exceeds its size limit, old entries are automatically removed before adding new ones:

```go
// Cache with 100 MB limit
indexCache, err := disk.NewIndexCache("/var/cache/blob/indexes",
    disk.WithMaxBytes(100 << 20),
)

// When adding a new entry that would exceed 100 MB:
// 1. Cache sorts entries by modification time (oldest first)
// 2. Removes oldest entries until there's room
// 3. Adds the new entry
```

### Manual Pruning

Prune a cache to a specific size:

```go
// Prune to 50 MB
freed, err := indexCache.Prune(50 << 20)
if err != nil {
    return err
}
fmt.Printf("Freed %d bytes\n", freed)
```

### Monitoring Cache Size

```go
// Get configured limit (0 = unlimited)
maxBytes := indexCache.MaxBytes()

// Get current size
currentBytes := indexCache.SizeBytes()

fmt.Printf("Index cache: %d / %d bytes (%.1f%%)\n",
    currentBytes, maxBytes,
    float64(currentBytes)/float64(maxBytes)*100,
)
```

## Cache Integrity

All caches automatically validate entries on read:

| Cache | Validation | On Failure |
|-------|------------|------------|
| RefCache | Format check (algorithm:hex) | Delete entry, return miss |
| ManifestCache | Digest verification + JSON parse | Delete entry, return miss |
| IndexCache | Digest verification | Delete entry, return miss |

This prevents cache poisoning attacks and handles filesystem corruption gracefully.

## Relationship to Content and Block Caches

The OCI client caches are distinct from the content and block caches:

| Cache Type | Purpose | Scope |
|------------|---------|-------|
| **RefCache** | Tag → digest mappings | OCI metadata |
| **ManifestCache** | Digest → manifest | OCI metadata |
| **IndexCache** | Digest → index blob | OCI metadata |
| **Content Cache** | Hash → file content | File deduplication |
| **Block Cache** | Range → data blocks | HTTP optimization |

Use OCI client caches to minimize registry API calls. Use content and block caches to minimize file data transfers.

### Complete Caching Setup

For maximum performance, combine all cache types:

```go
import (
    "context"
    "time"

    "github.com/meigma/blob/core"
    "github.com/meigma/blob/core/cache"
    "github.com/meigma/blob/client"
    "github.com/meigma/blob/client/cache/disk"
    contentdisk "github.com/meigma/blob/core/cache/disk"
)

func createFullyCachedClient() (*client.Client, cache.Cache, error) {
    // OCI client caches
    refCache, _ := disk.NewRefCache("/var/cache/blob/refs",
        disk.WithRefCacheTTL(5 * time.Minute),
    )
    manifestCache, _ := disk.NewManifestCache("/var/cache/blob/manifests")
    indexCache, _ := disk.NewIndexCache("/var/cache/blob/indexes")

    c := client.New(
        client.WithDockerConfig(),
        client.WithRefCache(refCache),
        client.WithManifestCache(manifestCache),
        client.WithIndexCache(indexCache),
    )

    // Content cache for file deduplication
    contentCache, err := contentdisk.New("/var/cache/blob/content",
        contentdisk.WithMaxBytes(1 << 30),  // 1 GB
    )
    if err != nil {
        return nil, nil, err
    }

    return c, contentCache, nil
}

func pullWithContentCache(c *client.Client, contentCache cache.Cache, ref string) (*blob.Blob, error) {
    // Pass content cache as a blob option during pull
    archive, err := c.Pull(context.Background(), ref,
        client.WithBlobOptions(blob.WithCache(contentCache)),
    )
    if err != nil {
        return nil, err
    }

    return archive, nil
}
```

## Sizing Guidelines

| Use Case | RefCache | ManifestCache | IndexCache |
|----------|----------|---------------|------------|
| Development | 1 MB | 5 MB | 20 MB |
| CI/CD | 5 MB | 20 MB | 100 MB |
| Production | 10 MB | 50 MB | 500 MB |

Manifests are typically 1-5 KB each. Index blobs range from 100 KB to several MB depending on archive file count.

## See Also

- [OCI Client](oci-client) - Client API for push/pull operations
- [Caching](caching) - Content-level caching for file deduplication
- [Block Caching](block-caching) - Block-level caching for HTTP sources
- [Performance Tuning](performance-tuning) - Cache tuning recommendations
