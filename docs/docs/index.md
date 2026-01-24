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
- **Supply chain security**: Sigstore signing and SLSA provenance with simple verification helpers
- **Directory fetches**: Efficiently retrieve all files in a directory with a single request
- **Content-addressed caching**: Automatic deduplication across archives

## Quick Start

### Using the Go Library

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

### Using the CLI

```bash
# Install
curl -sSfL https://blob.meigma.dev/install.sh | sh

# Push an archive
blob push --compression=zstd ghcr.io/myorg/myarchive:v1 ./src

# Read files lazily (HTTP range requests)
blob cat ghcr.io/myorg/myarchive:v1 config.json

# Pull entire archive
blob pull ghcr.io/myorg/myarchive:v1 ./dest
```

The pulled archive uses HTTP range requests to fetch file data on demand. Only the small index blob is downloaded immediately; file contents are fetched lazily when accessed.

## Next Steps

**Library users:**
- [Getting Started](./getting-started) - Go library tutorial
- [API Reference](./reference/api) - Complete Go API
- [OCI Client](./guides/oci-client) - Push and pull archives to registries

**CLI users:**
- [CLI Getting Started](./cli-getting-started) - Command-line tutorial
- [CLI Reference](./reference/cli) - Complete command reference
- [CLI Workflows](./guides/cli-workflows) - Signing, verification, and CI/CD patterns

**All users:**
- [Creating Archives](./guides/creating-archives) - Archive creation options
- [Provenance & Signing](./guides/provenance) - Sigstore signatures and SLSA attestations
- [Caching](./guides/caching) - Content-addressed caching for deduplication
