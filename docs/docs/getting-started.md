---
sidebar_position: 2
---

# Getting Started

This tutorial walks through the complete workflow of creating a blob archive, reading files from it, and extracting content. We will build a working example from scratch.

## Prerequisites

- Go 1.21 or later
- A directory with files to archive

## What We Will Build

We will create a simple program that:
1. Creates an archive from a source directory
2. Opens the archive and reads individual files
3. Lists directory contents
4. Extracts files to a destination

## Quick Start with Files

For working directly with local files, blob provides convenient helper functions that handle file I/O automatically:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/meigma/blob"
)

func main() {
	// Create an archive from a directory
	blobFile, err := blob.CreateBlob(context.Background(), "./src", "./archive",
		blob.CreateBlobWithCompression(blob.CompressionZstd),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer blobFile.Close()

	// Read a file
	content, err := blobFile.ReadFile("main.go")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Content: %s\n", content)
}
```

To open an existing archive:

```go
blobFile, err := blob.OpenFile("./archive/index.blob", "./archive/data.blob")
if err != nil {
	log.Fatal(err)
}
defer blobFile.Close()

// Use like any Blob
content, _ := blobFile.ReadFile("config.json")
```

The rest of this tutorial shows the lower-level API for more control over archive creation and data sources.

---

## Step 1: Create a Project

First, create a new directory and initialize a Go module:

```bash
mkdir blob-demo && cd blob-demo
go mod init blob-demo
go get github.com/meigma/blob
```

Create a `main.go` file:

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

Let's create some files to archive. Add this to your `run()` function:

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

Now we will create an archive from the source directory. The archive has two parts: an index (metadata) and data (file contents).

```go
// Create buffers for the archive
var indexBuf, dataBuf bytes.Buffer

// Create the archive
err = blob.Create(context.Background(), srcDir, &indexBuf, &dataBuf)
if err != nil {
	return fmt.Errorf("create archive: %w", err)
}

fmt.Printf("Archive created: index=%d bytes, data=%d bytes\n",
	indexBuf.Len(), dataBuf.Len())
```

## Step 4: Open the Archive

To read the archive, we need a ByteSource that provides random access to the data. For this tutorial, we will use an in-memory implementation:

```go
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
```

Add this to your `run()` function:

```go
// Create a byte source from our data buffer
source := &memSource{data: dataBuf.Bytes()}

// Open the archive
archive, err := blob.New(indexBuf.Bytes(), source)
if err != nil {
	return fmt.Errorf("open archive: %w", err)
}

fmt.Printf("Archive contains %d files\n", archive.Len())
```

## Step 5: Read Individual Files

Use `ReadFile()` to read file contents:

```go
// Read a specific file
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
```

## Step 6: List Directory Contents

Use `ReadDir()` to list files in a directory:

```go
// List root directory
entries, err := archive.ReadDir(".")
if err != nil {
	return fmt.Errorf("read dir: %w", err)
}

fmt.Println("\nRoot directory:")
for _, entry := range entries {
	typeStr := "file"
	if entry.IsDir() {
		typeStr = "dir "
	}
	fmt.Printf("  [%s] %s\n", typeStr, entry.Name())
}

// List a subdirectory
configEntries, err := archive.ReadDir("config")
if err != nil {
	return fmt.Errorf("read config dir: %w", err)
}

fmt.Println("\nconfig/ directory:")
for _, entry := range configEntries {
	fmt.Printf("  %s\n", entry.Name())
}
```

## Step 7: Extract Files

Use `CopyDir()` to extract files to a destination directory:

```go
// Create a destination directory
destDir, err := os.MkdirTemp("", "blob-dest-*")
if err != nil {
	return err
}
defer os.RemoveAll(destDir)

// Extract all files
err = archive.CopyDir(destDir, ".")
if err != nil {
	return fmt.Errorf("extract: %w", err)
}

fmt.Printf("\nExtracted files to %s\n", destDir)

// Verify extraction
extractedContent, err := os.ReadFile(filepath.Join(destDir, "readme.txt"))
if err != nil {
	return fmt.Errorf("read extracted: %w", err)
}
fmt.Printf("Verified readme.txt: %s\n", extractedContent)
```

## Complete Example

Here is the complete program:

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

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

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
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

	// Create buffers for the archive
	var indexBuf, dataBuf bytes.Buffer

	// Create the archive
	err = blob.Create(context.Background(), srcDir, &indexBuf, &dataBuf)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	fmt.Printf("Archive created: index=%d bytes, data=%d bytes\n",
		indexBuf.Len(), dataBuf.Len())

	// Create a byte source from our data buffer
	source := &memSource{data: dataBuf.Bytes()}

	// Open the archive
	archive, err := blob.New(indexBuf.Bytes(), source)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}

	fmt.Printf("Archive contains %d files\n", archive.Len())

	// Read a specific file
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

	// List root directory
	entries, err := archive.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	fmt.Println("\nRoot directory:")
	for _, entry := range entries {
		typeStr := "file"
		if entry.IsDir() {
			typeStr = "dir "
		}
		fmt.Printf("  [%s] %s\n", typeStr, entry.Name())
	}

	// List a subdirectory
	configEntries, err := archive.ReadDir("config")
	if err != nil {
		return fmt.Errorf("read config dir: %w", err)
	}

	fmt.Println("\nconfig/ directory:")
	for _, entry := range configEntries {
		fmt.Printf("  %s\n", entry.Name())
	}

	// Create a destination directory
	destDir, err := os.MkdirTemp("", "blob-dest-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(destDir)

	// Extract all files
	err = archive.CopyDir(destDir, ".")
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	fmt.Printf("\nExtracted files to %s\n", destDir)

	// Verify extraction
	extractedContent, err := os.ReadFile(filepath.Join(destDir, "readme.txt"))
	if err != nil {
		return fmt.Errorf("read extracted: %w", err)
	}
	fmt.Printf("Verified readme.txt: %s\n", extractedContent)

	return nil
}
```

Run the program:

```bash
go run main.go
```

Expected output:

```
Created 5 test files in /tmp/blob-src-123456
Archive created: index=680 bytes, data=162 bytes
Archive contains 5 files
readme.txt: Welcome to the blob demo!
config/app.json: {"name": "demo", "version": "1.0"}

Root directory:
  [dir ] config
  [file] readme.txt
  [dir ] src

config/ directory:
  app.json
  db.json

Extracted files to /tmp/blob-dest-789012
Verified readme.txt: Welcome to the blob demo!
```

## Next Steps

Now that you have the basics, explore these guides:

- [Creating Archives](guides/creating-archives) - Compression, change detection, and file limits
- [Working with Remote Archives](guides/remote-archives) - Access archives via HTTP range requests
- [Caching](guides/caching) - Speed up repeated access with content-addressed caching
- [Extracting Files](guides/extraction) - Advanced extraction options
- [Performance Tuning](guides/performance-tuning) - Optimize for your workload
