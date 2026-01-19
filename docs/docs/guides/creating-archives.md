---
sidebar_position: 1
---

# Creating Archives

How to build and push blob archives to OCI registries.

## Using Push (Recommended)

For most use cases, `Push` creates the archive and pushes it in a single call:

```go
import "github.com/meigma/blob"

c, err := blob.NewClient(blob.WithDockerConfig())
if err != nil {
	return err
}

err = c.Push(ctx, "ghcr.io/myorg/myarchive:v1", "./src",
	blob.PushWithCompression(blob.CompressionZstd),
)
```

This creates a compressed archive from the source directory and pushes it to the registry.

### Push Options

Control archive creation with push options:

```go
err = c.Push(ctx, ref, srcDir,
	// Compression
	blob.PushWithCompression(blob.CompressionZstd),
	blob.PushWithSkipCompression(blob.DefaultSkipCompression(1024)),

	// Build safety
	blob.PushWithChangeDetection(blob.ChangeDetectionStrict),
	blob.PushWithMaxFiles(100000),

	// Registry metadata
	blob.PushWithTags("latest", "stable"),
	blob.PushWithAnnotations(map[string]string{
		"org.opencontainers.image.version": "1.0.0",
	}),
)
```

---

## Compression

Enable zstd compression to reduce archive size:

```go
err = c.Push(ctx, ref, srcDir,
	blob.PushWithCompression(blob.CompressionZstd),
)
```

Compression reduces data size but requires decompression when reading. For typical source code and configuration files, expect 2-4x compression ratios.

Available compression options:
- `blob.CompressionNone` - Store files uncompressed (default)
- `blob.CompressionZstd` - Use zstd compression

## Skipping Compression

Some files compress poorly because they are already compressed (images, videos, archives) or too small to benefit. Use `PushWithSkipCompression` to skip these:

```go
err = c.Push(ctx, ref, srcDir,
	blob.PushWithCompression(blob.CompressionZstd),
	blob.PushWithSkipCompression(blob.DefaultSkipCompression(1024)),
)
```

`DefaultSkipCompression(minSize)` creates a predicate that skips:
- Files smaller than `minSize` bytes
- Files with known compressed extensions (`.jpg`, `.png`, `.zip`, `.gz`, etc.)

### Custom Skip Predicates

To define custom skip logic, pass additional predicates:

```go
// Skip lock files and generated code
skipGenerated := func(path string, info fs.FileInfo) bool {
	return strings.HasSuffix(path, ".lock") ||
		strings.Contains(path, "/generated/")
}

err = c.Push(ctx, ref, srcDir,
	blob.PushWithCompression(blob.CompressionZstd),
	blob.PushWithSkipCompression(
		blob.DefaultSkipCompression(1024),
		skipGenerated,
	),
)
```

If any predicate returns true, the file is stored uncompressed.

## Change Detection

For build pipelines, enable strict change detection to catch files that change during archive creation:

```go
err = c.Push(ctx, ref, srcDir,
	blob.PushWithChangeDetection(blob.ChangeDetectionStrict),
)
```

With strict change detection, the archive creation verifies that file size and modification time remain unchanged after reading. If a file changes mid-write, an error is returned rather than producing an archive with inconsistent content.

Change detection modes:
- `blob.ChangeDetectionNone` - No verification (default, fewer syscalls)
- `blob.ChangeDetectionStrict` - Verify files did not change during creation

## File Limits

To protect against runaway archive creation, limit the number of files:

```go
err = c.Push(ctx, ref, srcDir,
	blob.PushWithMaxFiles(50000),
)
```

If the source directory contains more files than the limit, the operation returns `blob.ErrTooManyFiles`.

Special values:
- `0` - Use default limit (200,000 files)
- Negative values - No limit

## Memory Considerations

Archive creation builds the entire index in memory before writing. Memory usage scales with the number of files and average path length.

Rough guide:
- 10,000 files: ~3-5 MB
- 100,000 files: ~30-50 MB
- 200,000 files: ~60-100 MB

For archives approaching the default 200,000 file limit, ensure the build environment has sufficient memory (256 MB+ recommended).

---

## Local Archives with CreateBlob

When you need local archive files without pushing to a registry, use `CreateBlob` from the core package:

```go
import blobcore "github.com/meigma/blob/core"

blobFile, err := blobcore.CreateBlob(ctx, srcDir, destDir,
	blobcore.CreateBlobWithCompression(blobcore.CompressionZstd),
)
if err != nil {
	return err
}
defer blobFile.Close()

// Use the archive immediately
content, _ := blobFile.ReadFile("config.json")
```

This creates `index.blob` and `data.blob` in `destDir` and returns an open archive.

### Custom Filenames

Override the default filenames:

```go
blobFile, err := blobcore.CreateBlob(ctx, srcDir, destDir,
	blobcore.CreateBlobWithIndexName("my-archive.idx"),
	blobcore.CreateBlobWithDataName("my-archive.dat"),
)
```

### Pushing a Pre-created Archive

If you have a local archive you want to push later:

```go
import (
	"github.com/meigma/blob"
	blobcore "github.com/meigma/blob/core"
)

// Create local archive
blobFile, _ := blobcore.CreateBlob(ctx, srcDir, destDir,
	blobcore.CreateBlobWithCompression(blobcore.CompressionZstd),
)
defer blobFile.Close()

// Push to registry
c, _ := blob.NewClient(blob.WithDockerConfig())
c.PushArchive(ctx, ref, blobFile.Blob,
	blob.PushWithTags("latest"),
)
```

### Saving a Remote Archive Locally

To save an in-memory or remote Blob to local files:

```go
// archive is a *blobcore.Blob from any source (e.g., pulled from registry)
err := archive.Save("/path/to/index.blob", "/path/to/data.blob")
```

---

## Complete Example

A production archive creation and push:

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/meigma/blob"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c, err := blob.NewClient(
		blob.WithDockerConfig(),
		blob.WithCacheDir("/tmp/blob-cache"),
	)
	if err != nil {
		log.Fatal(err)
	}

	err = c.Push(ctx, "ghcr.io/myorg/myarchive:v1.0.0", "./dist",
		blob.PushWithCompression(blob.CompressionZstd),
		blob.PushWithSkipCompression(blob.DefaultSkipCompression(1024)),
		blob.PushWithChangeDetection(blob.ChangeDetectionStrict),
		blob.PushWithMaxFiles(100000),
		blob.PushWithTags("latest"),
		blob.PushWithAnnotations(map[string]string{
			"org.opencontainers.image.version":  "1.0.0",
			"org.opencontainers.image.revision": "abc123",
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

## See Also

- [OCI Client](oci-client) - Client configuration and authentication
- [Advanced Usage](advanced) - Lower-level Create API for custom I/O pipelines
- [Architecture](../explanation/architecture) - How the archive format works
