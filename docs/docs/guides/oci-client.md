---
sidebar_position: 2
---

# OCI Client

How to push and pull blob archives to OCI container registries.

The `client` package provides a high-level API for working with blob archives in OCI registries. It handles authentication, manifest management, and lazy blob access via HTTP range requests.

## Creating a Client

Create a client with `client.New()`:

```go
import "github.com/meigma/blob/client"

c := client.New(client.WithDockerConfig())
```

The client uses ORAS under the hood and supports all standard OCI registries including Docker Hub, GitHub Container Registry (ghcr.io), Amazon ECR, Google Artifact Registry, and Azure Container Registry.

## Authentication Options

### Docker Config (Recommended)

Read credentials from `~/.docker/config.json`:

```go
c := client.New(client.WithDockerConfig())
```

This is the recommended approach for development and CI environments where Docker is already configured.

### Static Credentials

Provide username and password directly:

```go
import (
    "github.com/meigma/blob/client"
    "github.com/meigma/blob/client/oras"
)

c := client.New(
    client.WithOCIClient(oras.New(
        oras.WithStaticCredentials("ghcr.io", "username", "password"),
    )),
)
```

### Static Token

Use a bearer token directly:

```go
c := client.New(
    client.WithOCIClient(oras.New(
        oras.WithStaticToken("ghcr.io", "your-token"),
    )),
)
```

### Anonymous Access

For public registries that don't require authentication:

```go
c := client.New(
    client.WithOCIClient(oras.New(
        oras.WithAnonymous(),
    )),
)
```

## Push Operations

### Basic Push

Push a blob archive to a registry:

```go
import (
    "context"

    "github.com/meigma/blob"
    "github.com/meigma/blob/client"
)

func pushArchive(srcDir string) error {
    // Create the archive
    blobFile, err := blob.CreateBlob(context.Background(), srcDir, "/tmp/archive",
        blob.CreateBlobWithCompression(blob.CompressionZstd),
    )
    if err != nil {
        return err
    }
    defer blobFile.Close()

    // Push to registry
    c := client.New(client.WithDockerConfig())
    return c.Push(context.Background(), "ghcr.io/myorg/myarchive:v1.0.0", blobFile.Blob)
}
```

### Multiple Tags

Apply additional tags to the same manifest:

```go
err := c.Push(ctx, "ghcr.io/myorg/myarchive:v1.0.0", blobFile.Blob,
    client.WithTags("latest", "stable"),
)
```

### Custom Annotations

Add OCI annotations to the manifest:

```go
err := c.Push(ctx, "ghcr.io/myorg/myarchive:v1.0.0", blobFile.Blob,
    client.WithAnnotations(map[string]string{
        "org.opencontainers.image.source": "https://github.com/myorg/myrepo",
        "org.opencontainers.image.revision": "abc123",
    }),
)
```

The `org.opencontainers.image.created` annotation is set automatically if not provided.

## Pull Operations

### Basic Pull (Lazy Loading)

Pull returns a `*blob.Blob` with lazy data loading:

```go
func readFromRegistry(ref string) error {
    c := client.New(client.WithDockerConfig())

    archive, err := c.Pull(context.Background(), ref)
    if err != nil {
        return err
    }

    // Data is fetched on demand via HTTP range requests
    content, err := archive.ReadFile("config.json")
    if err != nil {
        return err
    }
    fmt.Printf("Content: %s\n", content)

    return nil
}
```

The pulled archive uses HTTP range requests to fetch file data on demand. Only the small index blob is downloaded immediately; file contents are fetched lazily when accessed.

### Blob Options

Pass options through to the created Blob:

```go
archive, err := c.Pull(ctx, ref,
    client.WithBlobOptions(
        blob.WithMaxFileSize(512 << 20),  // 512 MB limit
        blob.WithDecoderConcurrency(4),
    ),
)
```

### Index Size Limits

Limit the maximum index size to prevent memory exhaustion:

```go
archive, err := c.Pull(ctx, ref,
    client.WithMaxIndexSize(16 << 20),  // 16 MB limit (default: 8 MB)
)
```

### Skip Cache

Force a fresh fetch bypassing all caches:

```go
archive, err := c.Pull(ctx, ref,
    client.WithPullSkipCache(),
)
```

## Fetch Operations (Metadata Only)

Use `Fetch` to retrieve manifest metadata without downloading data:

```go
manifest, err := c.Fetch(ctx, "ghcr.io/myorg/myarchive:v1.0.0")
if err != nil {
    return err
}

fmt.Printf("Digest: %s\n", manifest.Digest())
fmt.Printf("Index size: %d bytes\n", manifest.IndexDescriptor().Size)
fmt.Printf("Data size: %d bytes\n", manifest.DataDescriptor().Size)
```

This is useful for checking if an archive exists or inspecting its metadata without the overhead of setting up lazy blob access.

### Skip Cache on Fetch

```go
manifest, err := c.Fetch(ctx, ref,
    client.WithSkipCache(),
)
```

## Tag Operations

Create or update a tag pointing to an existing manifest:

```go
// First, fetch the manifest to get its digest
manifest, err := c.Fetch(ctx, "ghcr.io/myorg/myarchive:v1.0.0")
if err != nil {
    return err
}

// Tag the same manifest with a new tag
err = c.Tag(ctx, "ghcr.io/myorg/myarchive:latest", manifest.Digest())
```

> **Note:** For tagging during push, use `client.WithTags()` which is more reliable as it applies tags atomically with the push operation.

## Error Handling

The client provides sentinel errors for common failure cases:

```go
import "errors"

archive, err := c.Pull(ctx, ref)
if err != nil {
    switch {
    case errors.Is(err, client.ErrNotFound):
        // Archive does not exist at this reference
        return fmt.Errorf("archive not found: %s", ref)

    case errors.Is(err, client.ErrInvalidReference):
        // Reference string is malformed
        return fmt.Errorf("invalid reference: %s", ref)

    case errors.Is(err, client.ErrInvalidManifest):
        // Manifest exists but is not a valid blob archive
        return fmt.Errorf("not a blob archive: %s", ref)

    default:
        return err
    }
}
```

### Available Errors

| Error | Description |
|-------|-------------|
| `ErrNotFound` | Archive does not exist at the reference |
| `ErrInvalidReference` | Reference string is malformed |
| `ErrInvalidManifest` | Manifest is not a valid blob archive manifest |
| `ErrMissingIndex` | Manifest does not contain an index blob |
| `ErrMissingData` | Manifest does not contain a data blob |

## Plain HTTP for Local Development

For local registries without TLS:

```go
c := client.New(
    client.WithPlainHTTP(true),
)

// Works with local registries like localhost:5000
err := c.Push(ctx, "localhost:5000/myarchive:latest", blobFile.Blob)
```

## Complete Example

A complete workflow pushing and pulling with caching:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/meigma/blob"
    "github.com/meigma/blob/client"
    "github.com/meigma/blob/client/cache/disk"
)

func main() {
    ctx := context.Background()

    // Create caches
    refCache, _ := disk.NewRefCache("/var/cache/blob/refs")
    manifestCache, _ := disk.NewManifestCache("/var/cache/blob/manifests")
    indexCache, _ := disk.NewIndexCache("/var/cache/blob/indexes")

    // Create client with caching
    c := client.New(
        client.WithDockerConfig(),
        client.WithRefCache(refCache),
        client.WithManifestCache(manifestCache),
        client.WithIndexCache(indexCache),
    )

    // Create and push an archive
    blobFile, err := blob.CreateBlob(ctx, "./src", "/tmp/archive",
        blob.CreateBlobWithCompression(blob.CompressionZstd),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer blobFile.Close()

    ref := "ghcr.io/myorg/myarchive:v1.0.0"
    if err := c.Push(ctx, ref, blobFile.Blob); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Pushed to %s\n", ref)

    // Pull and read lazily
    archive, err := c.Pull(ctx, ref)
    if err != nil {
        log.Fatal(err)
    }

    content, err := archive.ReadFile("main.go")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("main.go:\n%s\n", content)
}
```

## See Also

- [OCI Client Caching](oci-client-caching) - Configure client-level caches
- [Creating Archives](creating-archives) - Archive creation options
- [Caching](caching) - Content-level caching for file deduplication
- [OCI Storage](../explanation/oci-storage) - How blob archives are stored in OCI registries
