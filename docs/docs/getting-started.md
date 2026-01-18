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
1. Creates an archive from a source directory
2. Pushes the archive to an OCI registry
3. Pulls the archive and reads files lazily
4. Adds client caching for improved performance

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
	"github.com/meigma/blob/client"
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

## Step 3: Create the Archive

Create an archive from the source directory using `CreateBlob`:

```go
// Create a temporary directory for the archive
archiveDir, err := os.MkdirTemp("", "blob-archive-*")
if err != nil {
	return err
}
defer os.RemoveAll(archiveDir)

// Create the archive with zstd compression
blobFile, err := blob.CreateBlob(context.Background(), srcDir, archiveDir,
	blob.CreateBlobWithCompression(blob.CompressionZstd),
)
if err != nil {
	return fmt.Errorf("create archive: %w", err)
}
defer blobFile.Close()

fmt.Printf("Archive created: %d files\n", blobFile.Len())
```

## Step 4: Push to OCI Registry

Push the archive to a registry. Replace the reference with your own registry:

```go
// Create the OCI client
c := client.New(client.WithDockerConfig())

// Push to registry
// Replace with your registry: docker.io/username/demo:v1, ghcr.io/org/demo:v1, etc.
ref := "localhost:5000/blob-demo:v1"  // Use a local registry for testing
if err := c.Push(context.Background(), ref, blobFile.Blob); err != nil {
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
c := client.New(
	client.WithDockerConfig(),
	client.WithPlainHTTP(true),  // For local registries without TLS
)
```

## Step 5: Pull and Read Files Lazily

Pull the archive and read files. Data is fetched on demand via HTTP range requests:

```go
// Pull the archive (downloads only the small index blob)
archive, err := c.Pull(context.Background(), ref)
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

## Step 6: Add Client Caching

Add OCI client caches to avoid redundant registry requests:

```go
import "github.com/meigma/blob/client/cache/disk"

// Create caches
refCache, err := disk.NewRefCache("/tmp/blob-cache/refs")
if err != nil {
	return err
}

manifestCache, err := disk.NewManifestCache("/tmp/blob-cache/manifests")
if err != nil {
	return err
}

indexCache, err := disk.NewIndexCache("/tmp/blob-cache/indexes")
if err != nil {
	return err
}

// Create client with caching
c := client.New(
	client.WithDockerConfig(),
	client.WithPlainHTTP(true),
	client.WithRefCache(refCache),
	client.WithManifestCache(manifestCache),
	client.WithIndexCache(indexCache),
)

// Second pull will use cached data
archive, err := c.Pull(context.Background(), ref)
if err != nil {
	return err
}
fmt.Printf("Pulled (cached): %d files\n", archive.Len())
```

## Step 7: Add Content Caching (Optional)

For repeated file access, add content-level caching. Pass the cache when pulling the archive:

```go
import (
	"github.com/meigma/blob"
	"github.com/meigma/blob/cache/disk"
)

// Create content cache
contentCache, err := disk.New("/tmp/blob-cache/content")
if err != nil {
	return err
}

// Pull with content caching enabled
archive, err := c.Pull(context.Background(), ref,
	client.WithBlobOptions(blob.WithCache(contentCache)),
)
if err != nil {
	return err
}

// First read: fetches via network, caches result
content, err := archive.ReadFile("src/main.go")
if err != nil {
	return err
}
fmt.Printf("First read: %s\n", content)

// Second read: returns from cache, no network request
content, err = archive.ReadFile("src/main.go")
if err != nil {
	return err
}
fmt.Printf("Second read (cached): %s\n", content)
```

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
	contentdisk "github.com/meigma/blob/cache/disk"
	"github.com/meigma/blob/client"
	"github.com/meigma/blob/client/cache/disk"
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

	// Step 2: Create the archive
	archiveDir, err := os.MkdirTemp("", "blob-archive-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(archiveDir)

	blobFile, err := blob.CreateBlob(ctx, srcDir, archiveDir,
		blob.CreateBlobWithCompression(blob.CompressionZstd),
	)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer blobFile.Close()
	fmt.Printf("Archive created: %d files\n", blobFile.Len())

	// Step 3: Create cached client with content caching
	refCache, _ := disk.NewRefCache("/tmp/blob-cache/refs")
	manifestCache, _ := disk.NewManifestCache("/tmp/blob-cache/manifests")
	indexCache, _ := disk.NewIndexCache("/tmp/blob-cache/indexes")
	contentCache, _ := contentdisk.New("/tmp/blob-cache/content")

	c := client.New(
		client.WithDockerConfig(),
		client.WithPlainHTTP(true), // For local registry
		client.WithRefCache(refCache),
		client.WithManifestCache(manifestCache),
		client.WithIndexCache(indexCache),
	)

	// Step 4: Push to registry
	ref := "localhost:5000/blob-demo:v1"
	if err := c.Push(ctx, ref, blobFile.Blob); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	fmt.Printf("Pushed to %s\n", ref)

	// Step 5: Pull with content caching enabled
	archive, err := c.Pull(ctx, ref,
		client.WithBlobOptions(blob.WithCache(contentCache)),
	)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	fmt.Printf("Pulled: %d files\n", archive.Len())

	// Step 6: Read files (uses content cache automatically)
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
Archive created: 5 files
Pushed to localhost:5000/blob-demo:v1
Pulled: 5 files

readme.txt: Welcome to the blob demo!

config/ directory:
  app.json
  db.json
```

## Next Steps

Now that you have the basics, explore these guides:

- [OCI Client](guides/oci-client) - Authentication options and advanced client usage
- [OCI Client Caching](guides/oci-client-caching) - Configure client cache tiers
- [Creating Archives](guides/creating-archives) - Compression, change detection, and file limits
- [Caching](guides/caching) - Content caching for file deduplication
- [Extracting Files](guides/extraction) - Advanced extraction options
- [Performance Tuning](guides/performance-tuning) - Optimize for your workload

---

## Lower-Level API

For more control over archive creation and data sources, use the lower-level APIs:

### Create API

Write index and data to separate writers:

```go
import (
	"bytes"
	"context"

	"github.com/meigma/blob"
)

var indexBuf, dataBuf bytes.Buffer

err := blob.Create(context.Background(), srcDir, &indexBuf, &dataBuf,
	blob.CreateWithCompression(blob.CompressionZstd),
)
if err != nil {
	return err
}

fmt.Printf("Index: %d bytes, Data: %d bytes\n", indexBuf.Len(), dataBuf.Len())
```

### New API

Create a Blob from index data and a ByteSource:

```go
import (
	"io"

	"github.com/meigma/blob"
)

// memSource wraps a byte slice with random access
type memSource struct {
	data []byte
}

func (m *memSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	return n, nil
}

func (m *memSource) Size() int64 {
	return int64(len(m.data))
}

func (m *memSource) SourceID() string {
	return "memory"
}

// Create blob from index and data
source := &memSource{data: dataBuf.Bytes()}
archive, err := blob.New(indexBuf.Bytes(), source)
if err != nil {
	return err
}
```

### OpenFile API

Open local archive files directly:

```go
blobFile, err := blob.OpenFile("./archive/index.blob", "./archive/data.blob")
if err != nil {
	return err
}
defer blobFile.Close()

content, _ := blobFile.ReadFile("config.json")
```
