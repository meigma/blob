---
sidebar_position: 1
slug: /
---

# Blob

A file archive format designed for OCI container registries.

Blob enables random access to individual files via HTTP range requests without downloading entire archives. It's optimized for storing and retrieving files from OCI 1.1 container registries.

## Key Features

- **Random access**: Read any file without streaming the entire archive
- **Integrity**: Per-file SHA256 hashes protect against corruption
- **Directory fetches**: Efficiently retrieve all files in a directory with a single request
- **Content-addressed caching**: Automatic deduplication across archives

## Quick Start

```go
import "github.com/meigma/blob"
```

See the [Getting Started](./getting-started) tutorial for a complete walkthrough.
