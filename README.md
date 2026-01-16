# Blob

A file archive format for OCI container registries with random access via HTTP range requests.

Blob stores files in two OCI blobs: a small index containing metadata, and a data blob containing file contents sorted by path. This design enables reading individual files without downloading entire archives, efficient directory fetches with single range requests, and content-addressed caching with automatic deduplication.

## Installation

```bash
go get github.com/meigma/blob
```

Requires Go 1.25 or later.

## Usage

### Creating an Archive

```go
import (
    "context"
    "bytes"
    "github.com/meigma/blob"
)

var indexBuf, dataBuf bytes.Buffer
err := blob.Create(context.Background(), "/path/to/source", &indexBuf, &dataBuf)
```

### Reading Files

```go
import "github.com/meigma/blob"

// Open archive with index data and a ByteSource for the data blob
archive, err := blob.New(indexData, source)

// Read a file
content, err := archive.ReadFile("config/app.json")

// List directory contents
entries, err := archive.ReadDir("src")

// Use as fs.FS
f, err := archive.Open("main.go")
```

### Remote Archives via HTTP

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/http"
)

source, err := http.NewSource(dataURL,
    http.WithHeader("Authorization", "Bearer "+token),
)
archive, err := blob.New(indexData, source)
```

### Caching

```go
import (
    "github.com/meigma/blob/cache"
    "github.com/meigma/blob/cache/disk"
)

diskCache, err := disk.New("/var/cache/blob")
cached := cache.New(archive, diskCache)

// First read fetches from source and caches
content, err := cached.ReadFile("lib/utils.go")

// Second read returns from cache
content, err = cached.ReadFile("lib/utils.go")
```

## Features

- **Random access** - Read any file without downloading the entire archive
- **Directory fetches** - Path-sorted storage enables single-request directory reads
- **Integrity verification** - Per-file SHA256 hashes
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