---
sidebar_position: 1

---

# Blob

A file archive format designed for OCI container registries.

Blob enables random access to individual files via HTTP range requests without downloading entire archives. Push archives to any OCI registry and read files lazily with minimal network transfer.

## Key Features

- **OCI-native**: Push and pull archives to any OCI 1.1 registry
- **Lazy loading**: Read any file via HTTP range requests without downloading the entire archive
- **Integrity**: Per-file SHA256 hashes protect against corruption
- **Supply chain security**: Sigstore signing and SLSA provenance with OPA policy verification
- **Directory fetches**: Efficiently retrieve all files in a directory with a single request
- **Content-addressed caching**: Automatic deduplication across archives

## Quick Start

```go
import "github.com/meigma/blob"

// Create client and push an archive
c, _ := blob.NewClient(blob.WithDockerConfig())
c.Push(ctx, "ghcr.io/myorg/myarchive:v1", "./src",
    blob.PushWithCompression(blob.CompressionZstd),
)

// Pull and read files lazily
archive, _ := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
content, _ := archive.ReadFile("config.json")
```

The pulled archive uses HTTP range requests to fetch file data on demand. Only the small index blob is downloaded immediately; file contents are fetched lazily when accessed.

## Next Steps

See the [Getting Started](./getting-started) tutorial for a complete walkthrough, or jump directly to:

- [OCI Client](./guides/oci-client) - Push and pull archives to registries
- [Creating Archives](./guides/creating-archives) - Archive creation options
- [Provenance & Signing](./guides/provenance) - Sigstore signatures and SLSA attestations
- [Caching](./guides/caching) - Content-addressed caching for deduplication
