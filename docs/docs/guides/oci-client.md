---
sidebar_position: 2
---

# OCI Client

How to push and pull blob archives to OCI container registries.

The `blob` package provides a high-level API for working with blob archives in OCI registries. It handles authentication, manifest management, and lazy blob access via HTTP range requests.

## Creating a Client

Create a client with `blob.NewClient()`:

```go
import "github.com/meigma/blob"

c, err := blob.NewClient(blob.WithDockerConfig())
if err != nil {
	return err
}
```

The client uses ORAS under the hood and supports all standard OCI registries including Docker Hub, GitHub Container Registry (ghcr.io), Amazon ECR, Google Artifact Registry, and Azure Container Registry.

## Authentication Options

### Docker Config (Recommended)

Read credentials from `~/.docker/config.json`:

```go
c, _ := blob.NewClient(blob.WithDockerConfig())
```

This is the recommended approach for development and CI environments where Docker is already configured.

### Static Credentials

Provide username and password directly:

```go
c, _ := blob.NewClient(
	blob.WithStaticCredentials("ghcr.io", "username", "password"),
)
```

### Static Token

Use a bearer token directly:

```go
c, _ := blob.NewClient(
	blob.WithStaticToken("ghcr.io", "your-token"),
)
```

### Anonymous Access

For public registries that don't require authentication:

```go
c, _ := blob.NewClient(blob.WithAnonymous())
```

## Push Operations

### Basic Push

Push a directory to a registry as an archive:

```go
import "github.com/meigma/blob"

func pushArchive(srcDir string) error {
	c, err := blob.NewClient(blob.WithDockerConfig())
	if err != nil {
		return err
	}

	return c.Push(ctx, "ghcr.io/myorg/myarchive:v1.0.0", srcDir,
		blob.PushWithCompression(blob.CompressionZstd),
	)
}
```

#### CLI Equivalent

```bash
# Basic push
blob push ghcr.io/myorg/myarchive:v1.0.0 ./src

# With compression
blob push --compression=zstd ghcr.io/myorg/myarchive:v1.0.0 ./src
```

### Multiple Tags

Apply additional tags to the same manifest:

```go
err := c.Push(ctx, "ghcr.io/myorg/myarchive:v1.0.0", srcDir,
	blob.PushWithTags("latest", "stable"),
)
```

### Custom Annotations

Add OCI annotations to the manifest:

```go
err := c.Push(ctx, "ghcr.io/myorg/myarchive:v1.0.0", srcDir,
	blob.PushWithAnnotations(map[string]string{
		"org.opencontainers.image.source": "https://github.com/myorg/myrepo",
		"org.opencontainers.image.revision": "abc123",
	}),
)
```

The `org.opencontainers.image.created` annotation is set automatically if not provided.

### Pushing Pre-created Archives

If you have a pre-created archive from `blobcore.CreateBlob`:

```go
import (
	"github.com/meigma/blob"
	blobcore "github.com/meigma/blob/core"
)

blobFile, _ := blobcore.CreateBlob(ctx, srcDir, destDir,
	blobcore.CreateBlobWithCompression(blobcore.CompressionZstd),
)
defer blobFile.Close()

c, _ := blob.NewClient(blob.WithDockerConfig())
c.PushArchive(ctx, "ghcr.io/myorg/myarchive:v1.0.0", blobFile.Blob,
	blob.PushWithTags("latest"),
)
```

## Pull Operations

### Basic Pull (Lazy Loading)

Pull returns a `*blobcore.Blob` with lazy data loading:

```go
func readFromRegistry(ref string) error {
	c, err := blob.NewClient(blob.WithDockerConfig())
	if err != nil {
		return err
	}

	archive, err := c.Pull(ctx, ref)
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

#### CLI Equivalent

```bash
# Pull and read files
blob pull ghcr.io/myorg/myarchive:v1 ./dest

# Read a single file without extracting
blob cat ghcr.io/myorg/myarchive:v1 config.json
```

### Pull Options

Configure blob decoding and limits:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithMaxFileSize(512 << 20),      // 512 MB limit
	blob.PullWithDecoderConcurrency(4),       // Parallel decompression
	blob.PullWithMaxIndexSize(16 << 20),      // 16 MB index limit
)
```

### Skip Cache

Force a fresh fetch bypassing all caches:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithSkipCache(),
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
	blob.FetchWithSkipCache(),
)
```

## Inspect Operations (Metadata + File Index)

Use `Inspect` to retrieve both manifest metadata and the file index without downloading the data blob. This is more comprehensive than `Fetch` and lets you examine archive contents:

```go
result, err := c.Inspect(ctx, "ghcr.io/myorg/myarchive:v1.0.0")
if err != nil {
	return err
}

// Access manifest metadata
fmt.Printf("Digest: %s\n", result.Digest())
fmt.Printf("Created: %s\n", result.Created())

// Access archive statistics
fmt.Printf("Files: %d\n", result.FileCount())
fmt.Printf("Data blob size: %d bytes\n", result.DataBlobSize())
fmt.Printf("Uncompressed size: %d bytes\n", result.TotalUncompressedSize())
fmt.Printf("Compression ratio: %.2f\n", result.CompressionRatio())

// List all files without downloading any data
for entry := range result.Index().Entries() {
	fmt.Printf("  %s (%d bytes)\n", entry.Path(), entry.OriginalSize())
}
```

### Fetch vs Inspect vs Pull

| Operation | Downloads | Use Case |
|-----------|-----------|----------|
| `Fetch` | Manifest only | Check if archive exists, get digest |
| `Inspect` | Manifest + file index | Browse files, check sizes, pre-flight validation |
| `Pull` | Manifest + index (+ data on demand) | Read file contents |

### Fetching Referrers (Signatures, Attestations)

`Inspect` provides lazy access to referrer artifacts like Sigstore signatures and SLSA attestations:

```go
result, err := c.Inspect(ctx, ref)
if err != nil {
	return err
}

// Fetch all referrers
referrers, err := result.Referrers(ctx, "")
if err != nil {
	if errors.Is(err, blob.ErrReferrersUnsupported) {
		fmt.Println("Registry does not support referrers API")
	}
	return err
}

for _, ref := range referrers {
	fmt.Printf("Referrer: %s (type: %s)\n", ref.Digest, ref.ArtifactType)
}

// Filter by artifact type
signatures, _ := result.Referrers(ctx, "application/vnd.dev.sigstore.bundle.v0.3+json")
fmt.Printf("Found %d signatures\n", len(signatures))
```

### Inspect Options

```go
// Skip all caches for fresh data
result, err := c.Inspect(ctx, ref,
	blob.InspectWithSkipCache(),
)

// Limit index size (for untrusted registries)
result, err := c.Inspect(ctx, ref,
	blob.InspectWithMaxIndexSize(4 << 20), // 4 MB limit
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

> **Note:** For tagging during push, use `blob.PushWithTags()` which is more reliable as it applies tags atomically with the push operation.

## Sign Operations

Sign a manifest with Sigstore and attach the signature as an OCI 1.1 referrer:

```go
import "github.com/meigma/blob/policy/sigstore"

// Create a signer (keyless, for CI environments)
signer, err := sigstore.NewSigner(
	sigstore.WithEphemeralKey(),
	sigstore.WithFulcio("https://fulcio.sigstore.dev"),
	sigstore.WithRekor("https://rekor.sigstore.dev"),
	sigstore.WithAmbientCredentials(),
)
if err != nil {
	return err
}

// Sign the manifest (after pushing)
sigDigest, err := c.Sign(ctx, "ghcr.io/myorg/myarchive:v1.0.0", signer)
if err != nil {
	return err
}
fmt.Printf("Signature digest: %s\n", sigDigest)
```

The signature is attached as an OCI referrer artifact. Consumers can discover it via the Referrers API and verify it using a `sigstore.Policy`.

### Complete Push and Sign Workflow

```go
func pushAndSign(ctx context.Context, ref, srcDir string) error {
	c, err := blob.NewClient(blob.WithDockerConfig())
	if err != nil {
		return err
	}

	// Push the archive
	if err := c.Push(ctx, ref, srcDir,
		blob.PushWithCompression(blob.CompressionZstd),
	); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	// Create keyless signer
	signer, err := sigstore.NewSigner(
		sigstore.WithEphemeralKey(),
		sigstore.WithFulcio("https://fulcio.sigstore.dev"),
		sigstore.WithRekor("https://rekor.sigstore.dev"),
		sigstore.WithAmbientCredentials(),
	)
	if err != nil {
		return fmt.Errorf("create signer: %w", err)
	}

	// Sign the manifest
	if _, err := c.Sign(ctx, ref, signer); err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	return nil
}
```

For detailed signing options and verification, see [Provenance & Signing](provenance).

## Error Handling

The client provides sentinel errors for common failure cases:

```go
import "errors"

archive, err := c.Pull(ctx, ref)
if err != nil {
	switch {
	case errors.Is(err, blob.ErrNotFound):
		// Archive does not exist at this reference
		return fmt.Errorf("archive not found: %s", ref)

	case errors.Is(err, blob.ErrInvalidReference):
		// Reference string is malformed
		return fmt.Errorf("invalid reference: %s", ref)

	case errors.Is(err, blob.ErrInvalidManifest):
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
| `blob.ErrNotFound` | Archive does not exist at the reference |
| `blob.ErrInvalidReference` | Reference string is malformed |
| `blob.ErrInvalidManifest` | Manifest is not a valid blob archive manifest |
| `blob.ErrMissingIndex` | Manifest does not contain an index blob |
| `blob.ErrMissingData` | Manifest does not contain a data blob |
| `blob.ErrPolicyViolation` | Archive rejected by a configured policy |

## Plain HTTP for Local Development

For local registries without TLS:

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithPlainHTTP(true),
)

// Works with local registries like localhost:5000
err := c.Push(ctx, "localhost:5000/myarchive:latest", srcDir)
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
)

func main() {
	ctx := context.Background()

	// Create client with all caches enabled
	c, err := blob.NewClient(
		blob.WithDockerConfig(),
		blob.WithCacheDir("/var/cache/blob"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Push an archive
	ref := "ghcr.io/myorg/myarchive:v1.0.0"
	if err := c.Push(ctx, ref, "./src",
		blob.PushWithCompression(blob.CompressionZstd),
		blob.PushWithTags("latest"),
	); err != nil {
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

- [CLI Reference](../reference/cli) - Command-line equivalent operations
- [Creating Archives](creating-archives) - Archive creation options
- [Caching](caching) - Cache configuration and sizing
- [Provenance & Signing](provenance) - Signing archives and verification policies
- [OCI Storage](../explanation/oci-storage) - How blob archives are stored in OCI registries
