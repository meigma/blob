# Blob

A file archive format for OCI container registries with random access via HTTP range requests.

Blob stores files in two OCI blobs: a small index containing metadata, and a data blob containing file contents sorted by path. This design enables reading individual files without downloading entire archives, efficient directory fetches with single range requests, and content-addressed caching with automatic deduplication.

## Installation

```bash
go get github.com/meigma/blob
```

Requires Go 1.25 or later.

## Usage

### Push and Pull Archives

```go
import (
    "context"
    "github.com/meigma/blob"
)

ctx := context.Background()

// Create client with Docker credentials
c, err := blob.NewClient(blob.WithDockerConfig())

// Push a directory to registry
err = c.Push(ctx, "ghcr.io/myorg/myarchive:v1", "./src",
    blob.PushWithCompression(blob.CompressionZstd),
)

// Pull and read files lazily via HTTP range requests
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
content, err := archive.ReadFile("config/app.json")
entries, err := archive.ReadDir("src")
```

### Caching

```go
// Enable all caches with a single option
c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithCacheDir("/var/cache/blob"),
)

// Subsequent pulls and reads use cached data
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
```

### Low-Level Archive Creation

```go
import (
    "bytes"
    "context"
    blobcore "github.com/meigma/blob/core"
)

var indexBuf, dataBuf bytes.Buffer
err := blobcore.Create(context.Background(), "/path/to/source", &indexBuf, &dataBuf)
```

### Low-Level Archive Reading

```go
import (
    blobcore "github.com/meigma/blob/core"
    blobhttp "github.com/meigma/blob/core/http"
)

// Create HTTP source for data blob
source, err := blobhttp.NewSource(dataURL,
    blobhttp.WithHeader("Authorization", "Bearer "+token),
)

// Open archive with index data and byte source
archive, err := blobcore.New(indexData, source)
content, err := archive.ReadFile("config/app.json")
```

### Supply Chain Security

Verify archive provenance with Sigstore signatures and SLSA attestations:

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy"
    "github.com/meigma/blob/policy/sigstore"
    "github.com/meigma/blob/policy/slsa"
)

// Verify signatures and provenance from GitHub Actions
sigPolicy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main"),
    sigstore.AllowTags("v*"),
)
slsaPolicy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo")

c, _ := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy)),
)

// Pull fails if verification fails
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
```

## Features

- **Random access** - Read any file without downloading the entire archive
- **Directory fetches** - Path-sorted storage enables single-request directory reads
- **Integrity verification** - Per-file SHA256 hashes
- **Supply chain security** - Sigstore signing and SLSA provenance with simple verification helpers
- **Compression** - Per-file zstd compression preserves random access
- **Content-addressed caching** - Automatic deduplication across archives
- **Standard interfaces** - Implements `fs.FS`, `fs.ReadFileFS`, `fs.ReadDirFS`

## Documentation

Full documentation is available at the [documentation site](docs/docs/index.md):

- [Getting Started](docs/docs/getting-started.md) - Complete tutorial
- [Architecture](docs/docs/explanation/architecture.md) - Design decisions and trade-offs
- [API Reference](docs/docs/reference/api.md) - Complete API documentation

## License

Licensed under either of [Apache License, Version 2.0](LICENSE-APACHE) or [MIT License](LICENSE-MIT) at your option.