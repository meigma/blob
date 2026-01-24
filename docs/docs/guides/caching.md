---
sidebar_position: 3
---

# Caching

How to configure caching for blob archives to minimize network requests and improve performance.

## Quick Setup (Recommended)

For most users, a single option enables all cache layers:

```go
c, err := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/var/cache/blob"),
)
```

This creates a complete caching hierarchy under the specified directory with sensible defaults. The cache directory structure is:

```
/var/cache/blob/
├── refs/        # Tag → digest mappings (TTL-based)
├── manifests/   # Digest → manifest (content-addressed)
├── indexes/     # Digest → index blob (content-addressed)
├── content/     # Hash → file content (deduplication)
└── blocks/      # Block-level cache (HTTP optimization)
```

## Cache Architecture

Blob supports five cache layers that work together:

```
┌─────────────────────────────────────────────────────────────────┐
│                         Cache Layers                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │              OCI Metadata Caches (3 layers)               │  │
│  │  ┌──────────┐   ┌───────────────┐   ┌──────────────────┐  │  │
│  │  │ RefCache │ → │ ManifestCache │ → │    IndexCache    │  │  │
│  │  │ tag→dgst │   │ dgst→manifest │   │    dgst→index    │  │  │
│  │  └──────────┘   └───────────────┘   └──────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │               Data Caches (2 layers)                      │  │
│  │  ┌─────────────────────┐   ┌────────────────────────────┐ │  │
│  │  │    ContentCache     │   │        BlockCache          │ │  │
│  │  │  hash→file content  │   │   range→data blocks        │ │  │
│  │  └─────────────────────┘   └────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

| Cache | Purpose | Key | When to Use |
|-------|---------|-----|-------------|
| **RefCache** | Avoid tag resolution requests | tag → digest | Always (default 15 min TTL) |
| **ManifestCache** | Avoid manifest fetches | digest → manifest | Always (content-addressed) |
| **IndexCache** | Avoid index blob downloads | digest → bytes | Always (content-addressed) |
| **ContentCache** | Deduplicate file content | SHA256 → content | Repeated file access |
| **BlockCache** | Optimize HTTP range requests | source+offset → block | Random access patterns |

## Configuration Options

### RefCache TTL

Control how long tag→digest mappings are cached:

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/var/cache/blob"),
	blob.WithRefCacheTTL(5 * time.Minute),  // Refresh tags every 5 minutes
)
```

For mutable tags like `latest` that change frequently, use shorter TTLs. For immutable tags (semver releases), use longer TTLs or `0` to disable expiration.

### Individual Cache Directories

Place caches on different storage:

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithContentCacheDir("/fast-ssd/content"),  // SSD for content
	blob.WithBlockCacheDir("/fast-ssd/blocks"),     // SSD for blocks
	blob.WithRefCacheDir("/hdd/refs"),              // HDD for metadata
	blob.WithManifestCacheDir("/hdd/manifests"),
	blob.WithIndexCacheDir("/hdd/indexes"),
)
```

### Disabling Specific Caches

Omit specific cache directories to disable them:

```go
// Only enable metadata caches, no content caching
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithRefCacheDir("/var/cache/blob/refs"),
	blob.WithManifestCacheDir("/var/cache/blob/manifests"),
	blob.WithIndexCacheDir("/var/cache/blob/indexes"),
)
```

## How Caching Works

### Pull Operation Flow

```
Pull("ghcr.io/org/repo:v1")
    │
    ▼
RefCache hit? ─Yes─▶ Use cached digest
    │ No
    ▼
HEAD request → Get digest → Cache in RefCache
    │
    ▼
ManifestCache hit? ─Yes─▶ Use cached manifest
    │ No
    ▼
GET manifest → Parse → Cache in ManifestCache
    │
    ▼
IndexCache hit? ─Yes─▶ Use cached index
    │ No
    ▼
GET index blob → Cache in IndexCache
    │
    ▼
Return *Blob (data loaded lazily)
```

### File Read Flow

```
archive.ReadFile("config.json")
    │
    ▼
ContentCache hit? ─Yes─▶ Return cached content
    │ No
    ▼
BlockCache hit for range? ─Yes─▶ Use cached blocks
    │ No
    ▼
HTTP Range Request → Cache in BlockCache
    │
    ▼
Decompress → Verify hash → Cache in ContentCache
    │
    ▼
Return content
```

## Cache Sizing

### Sizing Guidelines

| Use Case | Recommended Size | Notes |
|----------|-----------------|-------|
| Development | 256 MB - 1 GB | Balance performance with disk |
| CI/CD (ephemeral) | Unlimited | Disk reclaimed after job |
| Production server | 2-10 GB | Based on working set |
| Memory-constrained | 64-128 MB | Minimum useful size |

### Sizing by Cache Type

| Cache | Typical Entry Size | Sizing Notes |
|-------|-------------------|--------------|
| RefCache | ~100 bytes | Small; 10 MB holds 100K+ refs |
| ManifestCache | 1-5 KB | 50 MB holds 10K-50K manifests |
| IndexCache | 100 KB - 5 MB | Varies by file count |
| ContentCache | File sizes | Most disk usage |
| BlockCache | 64 KB blocks | Temporary; auto-prunes |

## Cache Integrity

All caches validate entries on read:

| Cache | Validation | On Failure |
|-------|------------|------------|
| RefCache | Format check | Delete, return miss |
| ManifestCache | Digest + JSON parse | Delete, return miss |
| IndexCache | Digest verification | Delete, return miss |
| ContentCache | SHA256 verification | Delete, return miss |
| BlockCache | No verification | Re-fetch |

This prevents cache poisoning and handles filesystem corruption gracefully.

## Bypassing Cache

Force fresh fetches when needed:

```go
// Pull without using any caches
archive, err := c.Pull(ctx, ref,
	blob.PullWithSkipCache(),
)

// Fetch manifest without cache
manifest, err := c.Fetch(ctx, ref,
	blob.FetchWithSkipCache(),
)
```

### CLI: Cache Management

```bash
# Enable caching
blob config set cache.dir ~/.cache/blob

# View cache status
blob cache status

# Clear all caches
blob cache clear

# Clear specific cache layer
blob cache clear content   # File content cache
blob cache clear blocks    # HTTP range block cache
blob cache clear refs      # Tag resolution cache

# Set reference cache TTL
blob config set cache.ref_ttl 5m
```

## Cache Lifecycle

### Automatic Pruning

When caches exceed their limits, old entries are automatically removed using LRU-style eviction (oldest entries removed first based on modification time).

### Sharing Across Processes

All disk caches are safe for concurrent access from multiple processes. They use atomic file operations and handle race conditions correctly.

### Persistence

Caches persist across program restarts. Content-addressed caches (ManifestCache, IndexCache, ContentCache) never become stale. RefCache entries expire based on TTL.

## Complete Example

A production setup with all caches and custom TTL:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/meigma/blob"
)

func main() {
	ctx := context.Background()

	// Create client with full caching
	c, err := blob.NewClient(
		blob.WithDockerConfig(),
		blob.WithCacheDir("/var/cache/blob"),
		blob.WithRefCacheTTL(5 * time.Minute),
	)
	if err != nil {
		log.Fatal(err)
	}

	// First pull: fetches from registry, populates caches
	archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Pulled: %d files\n", archive.Len())

	// Second pull: uses all caches, minimal network
	archive2, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cached pull: %d files\n", archive2.Len())

	// File reads use content cache
	content, _ := archive.ReadFile("config.json")
	fmt.Printf("config.json: %s\n", content)
}
```

## See Also

- [CLI Reference](../reference/cli#blob-cache) - Command-line cache management
- [CLI Workflows](cli-workflows#cache-management) - CLI caching patterns
- [OCI Client](oci-client) - Client configuration and authentication
- [Performance Tuning](performance-tuning) - Cache tuning for specific scenarios
- [Advanced Usage](advanced) - Custom cache implementations
