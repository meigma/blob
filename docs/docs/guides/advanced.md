---
sidebar_position: 9
---

# Advanced Usage

For most users, `github.com/meigma/blob` provides everything needed. This guide covers advanced patterns that require internal packages.

## When to Use Internal Packages

Use internal packages when you need:

- **Local-only archives**: Create archives without pushing to a registry
- **Custom data sources**: Use archives from HTTP, S3, or custom sources
- **Custom cache implementations**: Implement your own caching strategy
- **Direct registry operations**: Bypass the high-level client
- **Lower-level control**: Fine-grained control over archive creation

For standard push/pull workflows, prefer the unified API:

```go
import "github.com/meigma/blob"

c, _ := blob.NewClient(blob.WithDockerConfig())
c.Push(ctx, ref, srcDir)
archive, _ := c.Pull(ctx, ref)
```

---

## Local-Only Archives (No Registry)

Create archives directly to disk without pushing to a registry:

```go
import blobcore "github.com/meigma/blob/core"

// Create archive files in destDir
blobFile, err := blobcore.CreateBlob(ctx, srcDir, destDir,
	blobcore.CreateBlobWithCompression(blobcore.CompressionZstd),
	blobcore.CreateBlobWithChangeDetection(blobcore.ChangeDetectionStrict),
)
if err != nil {
	return err
}
defer blobFile.Close()

// Use the archive immediately
content, _ := blobFile.ReadFile("config.json")

// Files created:
// - destDir/index.blob
// - destDir/data.blob
```

### Custom Filenames

```go
blobFile, err := blobcore.CreateBlob(ctx, srcDir, destDir,
	blobcore.CreateBlobWithIndexName("my-archive.idx"),
	blobcore.CreateBlobWithDataName("my-archive.dat"),
)
```

### Opening Existing Local Archives

```go
blobFile, err := blobcore.OpenFile("/path/to/index.blob", "/path/to/data.blob")
if err != nil {
	return err
}
defer blobFile.Close()

content, _ := blobFile.ReadFile("lib/utils.go")
```

---

## Custom HTTP Sources

Fetch archives from any HTTP endpoint with range request support:

```go
import (
	blobcore "github.com/meigma/blob/core"
	blobhttp "github.com/meigma/blob/core/http"
)

// Create HTTP source with authentication
source, err := blobhttp.NewSource(dataURL,
	blobhttp.WithHeader("Authorization", "Bearer "+token),
)
if err != nil {
	return err
}

// Read index blob separately (e.g., from another URL or embedded)
indexData, _ := fetchIndexBlob(indexURL)

// Create archive from HTTP source
archive, err := blobcore.New(indexData, source)
if err != nil {
	return err
}

// File reads use HTTP range requests
content, _ := archive.ReadFile("config.json")
```

### Custom Source ID

For block caching, provide a stable source identifier:

```go
source, err := blobhttp.NewSource(dataURL,
	blobhttp.WithSourceID("myapp:archive:v1.2.3"),
)
```

---

## Custom Cache Implementations

Implement the `cache.Cache` interface for custom caching strategies:

```go
import "io/fs"

type Cache interface {
	// Get returns an fs.File for reading cached content.
	// Returns nil, false if content is not cached.
	Get(hash []byte) (fs.File, bool)

	// Put stores content by reading from the provided fs.File.
	Put(hash []byte, f fs.File) error

	// Delete removes cached content for the given hash.
	Delete(hash []byte) error

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes entries until the cache is at or below targetBytes.
	Prune(targetBytes int64) (int64, error)
}
```

### Example: Redis Cache

```go
type RedisCache struct {
	client *redis.Client
	maxBytes int64
}

func (c *RedisCache) Get(hash []byte) (fs.File, bool) {
	key := hex.EncodeToString(hash)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	return &bytesFile{data: data}, true
}

func (c *RedisCache) Put(hash []byte, f fs.File) error {
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	key := hex.EncodeToString(hash)
	return c.client.Set(ctx, key, data, 0).Err()
}

// ... implement remaining methods
```

### Using Custom Cache

```go
import blobcore "github.com/meigma/blob/core"

archive, err := blobcore.New(indexData, source,
	blobcore.WithCache(myRedisCache),
)
```

---

## Block Cache for HTTP Sources

Add block-level caching to reduce HTTP range requests:

```go
import (
	blobcore "github.com/meigma/blob/core"
	blobhttp "github.com/meigma/blob/core/http"
	"github.com/meigma/blob/core/cache"
	"github.com/meigma/blob/core/cache/disk"
)

// Create HTTP source
source, err := blobhttp.NewSource(dataURL)
if err != nil {
	return err
}

// Create block cache
blockCache, err := disk.NewBlockCache("/var/cache/blob-blocks",
	disk.WithBlockMaxBytes(256 << 20),  // 256 MB limit
)
if err != nil {
	return err
}

// Wrap source with caching
cachedSource, err := blockCache.Wrap(source,
	cache.WithBlockSize(64 << 10),     // 64 KB blocks
	cache.WithMaxBlocksPerRead(4),     // Bypass large sequential reads
)
if err != nil {
	return err
}

// Use cached source
archive, err := blobcore.New(indexData, cachedSource)
```

---

## Direct Registry Operations

Use the registry package for lower-level OCI operations:

```go
import "github.com/meigma/blob/registry"

// Create registry client
reg := registry.New(
	registry.WithDockerConfig(),
)

// Fetch manifest metadata
manifest, err := reg.Fetch(ctx, "ghcr.io/myorg/myarchive:v1")
if err != nil {
	return err
}

// Access manifest details
digest := manifest.Digest()
indexDesc := manifest.IndexDescriptor()
dataDesc := manifest.DataDescriptor()

fmt.Printf("Digest: %s\n", digest)
fmt.Printf("Index size: %d bytes\n", indexDesc.Size)
fmt.Printf("Data size: %d bytes\n", dataDesc.Size)

// Pull full archive with options
archive, err := reg.Pull(ctx, ref,
	registry.PullWithMaxFileSize(512 << 20),
)

// Tag an existing manifest with a new tag
err = reg.Tag(ctx, "ghcr.io/myorg/myarchive:latest", manifest.Digest())
```

---

## Lower-Level Create API

For streaming archive creation to arbitrary writers:

```go
import blobcore "github.com/meigma/blob/core"

// Create to arbitrary io.Writers
indexFile, _ := os.Create("index.blob")
dataFile, _ := os.Create("data.blob")
defer indexFile.Close()
defer dataFile.Close()

err := blobcore.Create(ctx, srcDir, indexFile, dataFile,
	blobcore.CreateWithCompression(blobcore.CompressionZstd),
	blobcore.CreateWithSkipCompression(blobcore.DefaultSkipCompression(1024)),
	blobcore.CreateWithChangeDetection(blobcore.ChangeDetectionStrict),
	blobcore.CreateWithMaxFiles(100000),
)
```

---

## Package Reference

| Package | Purpose |
|---------|---------|
| `github.com/meigma/blob` | **Primary API** - client, push, pull |
| `github.com/meigma/blob/core` | Archive creation and reading |
| `github.com/meigma/blob/core/cache` | Cache interfaces |
| `github.com/meigma/blob/core/cache/disk` | Disk cache implementations |
| `github.com/meigma/blob/core/http` | HTTP byte source |
| `github.com/meigma/blob/registry` | OCI registry operations |
| `github.com/meigma/blob/registry/cache` | Registry cache implementations |
| `github.com/meigma/blob/policy/sigstore` | Sigstore signature verification |
| `github.com/meigma/blob/policy/opa` | OPA policy evaluation |

## See Also

- [OCI Client](oci-client) - High-level client API
- [Caching](caching) - Standard cache configuration
- [Creating Archives](creating-archives) - Archive options
- [API Reference](../reference/api) - Complete API documentation
