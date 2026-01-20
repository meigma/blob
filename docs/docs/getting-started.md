---
sidebar_position: 2
---

# Getting Started

This tutorial walks through the complete workflow of creating a blob archive, pushing it to an OCI registry, and reading files lazily via HTTP range requests.

## Prerequisites

- Go 1.21 or later
- Access to an OCI registry (Docker Hub, ghcr.io, or a local registry)
- Docker configured with registry credentials (for `WithDockerConfig()`)

## What We Will Build

We will create a simple program that:
1. Creates and pushes an archive in a single call
2. Inspects archive metadata without downloading data
3. Pulls the archive and reads files lazily
4. Adds caching for improved performance

## Step 1: Create a Project

Create a new directory and initialize a Go module:

```bash
mkdir blob-demo && cd blob-demo
go mod init blob-demo
go get github.com/meigma/blob
```

Create a `main.go` file:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/meigma/blob"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// We'll fill this in step by step
	return nil
}
```

## Step 2: Create Test Files

Create some files to archive. Add this to your `run()` function:

```go
// Create a temporary source directory with test files
srcDir, err := os.MkdirTemp("", "blob-src-*")
if err != nil {
	return err
}
defer os.RemoveAll(srcDir)

// Create some test files
files := map[string]string{
	"readme.txt":       "Welcome to the blob demo!",
	"config/app.json":  `{"name": "demo", "version": "1.0"}`,
	"config/db.json":   `{"host": "localhost", "port": 5432}`,
	"src/main.go":      "package main\n\nfunc main() {}\n",
	"src/utils/log.go": "package utils\n\nfunc Log(msg string) {}\n",
}

for path, content := range files {
	fullPath := filepath.Join(srcDir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return err
	}
}

fmt.Printf("Created %d test files in %s\n", len(files), srcDir)
```

## Step 3: Push to OCI Registry

Create a client and push the archive. The `Push` method creates the archive and pushes it in a single call:

```go
ctx := context.Background()

// Create the OCI client
c, err := blob.NewClient(blob.WithDockerConfig())
if err != nil {
	return fmt.Errorf("create client: %w", err)
}

// Push to registry - creates archive from srcDir and pushes in one call
// Replace with your registry: docker.io/username/demo:v1, ghcr.io/org/demo:v1, etc.
ref := "localhost:5000/blob-demo:v1"  // Use a local registry for testing
if err := c.Push(ctx, ref, srcDir,
	blob.PushWithCompression(blob.CompressionZstd),
); err != nil {
	return fmt.Errorf("push archive: %w", err)
}

fmt.Printf("Pushed to %s\n", ref)
```

For local testing without a real registry, you can run a local registry:

```bash
docker run -d -p 5000:5000 --name registry registry:2
```

> **Note (macOS):** Port 5000 may conflict with AirPlay Receiver. Use port 5001 instead:
> `docker run -d -p 5001:5000 --name registry registry:2` and update references to `localhost:5001`.

And configure the client to use plain HTTP:

```go
c, err := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithPlainHTTP(true),  // For local registries without TLS
)
```

## Step 4: Inspect Archive Metadata

Before pulling the full archive, you can inspect its metadata without downloading the data blob:

```go
// Inspect fetches only manifest and file index (no data blob)
result, err := c.Inspect(ctx, ref)
if err != nil {
	return fmt.Errorf("inspect archive: %w", err)
}

fmt.Printf("Archive digest: %s\n", result.Digest())
fmt.Printf("Files: %d\n", result.FileCount())
fmt.Printf("Data size: %d bytes\n", result.DataBlobSize())
fmt.Printf("Compression ratio: %.2f\n", result.CompressionRatio())

// List all files without downloading any data
fmt.Println("\nFiles in archive:")
for entry := range result.Index().Entries() {
	fmt.Printf("  %s (%d bytes)\n", entry.Path(), entry.OriginalSize())
}
```

This is useful for checking archive contents before deciding to pull, or for building file browsers that don't need the actual file data.

## Step 5: Pull and Read Files Lazily

Pull the archive and read files. Data is fetched on demand via HTTP range requests:

```go
// Pull the archive (downloads only the small index blob)
archive, err := c.Pull(ctx, ref)
if err != nil {
	return fmt.Errorf("pull archive: %w", err)
}

fmt.Printf("Pulled archive with %d files\n", archive.Len())

// Read a specific file (fetches only this file's bytes via range request)
content, err := archive.ReadFile("readme.txt")
if err != nil {
	return fmt.Errorf("read file: %w", err)
}
fmt.Printf("readme.txt: %s\n", content)

// Read another file
configContent, err := archive.ReadFile("config/app.json")
if err != nil {
	return fmt.Errorf("read config: %w", err)
}
fmt.Printf("config/app.json: %s\n", configContent)

// List directory contents
entries, err := archive.ReadDir("config")
if err != nil {
	return fmt.Errorf("read dir: %w", err)
}
fmt.Println("\nconfig/ directory:")
for _, entry := range entries {
	fmt.Printf("  %s\n", entry.Name())
}
```

## Step 6: Add Caching

Add caching with a single option. `WithCacheDir` enables all cache layers:

```go
// Create client with all caches enabled in one line
c, err := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithPlainHTTP(true),
	blob.WithCacheDir("/tmp/blob-cache"),
)
if err != nil {
	return err
}

// Second pull will use cached data - no network requests needed
archive, err := c.Pull(ctx, ref)
if err != nil {
	return err
}
fmt.Printf("Pulled (cached): %d files\n", archive.Len())

// Cached file reads are instant
content, err := archive.ReadFile("src/main.go")
if err != nil {
	return err
}
fmt.Printf("src/main.go: %s\n", content)
```

The cache directory structure is:
- `content/` - file content cache (deduplication across archives)
- `blocks/` - HTTP range block cache
- `refs/` - tag→digest mappings
- `manifests/` - parsed manifests
- `indexes/` - index blobs

## Complete Example

Here is the complete program:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/meigma/blob"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	// Step 1: Create test files
	srcDir, err := os.MkdirTemp("", "blob-src-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(srcDir)

	files := map[string]string{
		"readme.txt":       "Welcome to the blob demo!",
		"config/app.json":  `{"name": "demo", "version": "1.0"}`,
		"config/db.json":   `{"host": "localhost", "port": 5432}`,
		"src/main.go":      "package main\n\nfunc main() {}\n",
		"src/utils/log.go": "package utils\n\nfunc Log(msg string) {}\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("Created %d test files\n", len(files))

	// Step 2: Create cached client
	c, err := blob.NewClient(
		blob.WithDockerConfig(),
		blob.WithPlainHTTP(true), // For local registry
		blob.WithCacheDir("/tmp/blob-cache"),
	)
	if err != nil {
		return err
	}

	// Step 3: Push to registry (creates archive and pushes in one call)
	ref := "localhost:5000/blob-demo:v1"
	if err := c.Push(ctx, ref, srcDir,
		blob.PushWithCompression(blob.CompressionZstd),
	); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	fmt.Printf("Pushed to %s\n", ref)

	// Step 4: Pull and read files
	archive, err := c.Pull(ctx, ref)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	fmt.Printf("Pulled: %d files\n", archive.Len())

	content, _ := archive.ReadFile("readme.txt")
	fmt.Printf("\nreadme.txt: %s\n", content)

	entries, _ := archive.ReadDir("config")
	fmt.Println("\nconfig/ directory:")
	for _, entry := range entries {
		fmt.Printf("  %s\n", entry.Name())
	}

	return nil
}
```

Run the program (with a local registry running):

```bash
# Start local registry
docker run -d -p 5000:5000 --name registry registry:2

# Run the demo
go run main.go
```

Expected output:

```
Created 5 test files
Pushed to localhost:5000/blob-demo:v1
Pulled: 5 files

readme.txt: Welcome to the blob demo!

config/ directory:
  app.json
  db.json
```

## Next Steps

Now that you have the basics, explore these guides:

- [OCI Client](guides/oci-client) - Authentication options and client configuration
- [Creating Archives](guides/creating-archives) - Compression, change detection, and file limits
- [Caching](guides/caching) - Cache configuration and sizing
- [Extracting Files](guides/extraction) - Advanced extraction options
- [Performance Tuning](guides/performance-tuning) - Optimize for your workload

## Production Security

For production deployments, add supply chain security to verify archive provenance:

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy"
    "github.com/meigma/blob/policy/sigstore"
    "github.com/meigma/blob/policy/slsa"
)

// Verify signatures from GitHub Actions
sigPolicy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main"),
    sigstore.AllowTags("v*"),
)

// Validate SLSA provenance
slsaPolicy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowPath(".github/workflows/release.yml"),
)

// Combine policies (both must pass)
c, _ := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy)),
)

// Pull rejects archives that fail verification
archive, err := c.Pull(ctx, ref)
```

This ensures archives are signed by trusted workflows and built through authorized CI/CD pipelines.

See the [Provenance & Signing](guides/provenance) guide for complete implementation details, including custom OPA policies for advanced use cases.
