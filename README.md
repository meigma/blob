# Blob

[![CI](https://github.com/meigma/blob/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/meigma/blob/actions/workflows/ci.yml)
[![Docs](https://img.shields.io/badge/docs-blob.meigma.dev-blue)](https://blob.meigma.dev)
[![Go Reference](https://pkg.go.dev/badge/github.com/meigma/blob.svg)](https://pkg.go.dev/github.com/meigma/blob)
[![Release](https://img.shields.io/github/v/release/meigma/blob)](https://github.com/meigma/blob/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0%2FMIT-blue)](LICENSE-MIT)

> Sign and attest file archives in OCI registries. Carry cryptographic provenance wherever they go.

You sign your container images. What about everything else? Config files, ML models, deployment artifacts, and certificates move between systems with no provenance, no integrity verification, and full downloads every time. Blob brings the same supply chain security guarantees to file archives.

## How It Works

Blob stores archives as two OCI blobs bound by a signed manifest:

```
Signed → Manifest → Index → Per-file SHA256
```

Every file inherits the signature above it. Tamper with a single byte and verification fails instantly.

The index blob is small (~1MB for 10K files) and contains file metadata. The data blob stores file contents sorted by path. This separation enables reading individual files via HTTP range requests without downloading entire archives—read a 64KB config from a 1GB archive and transfer only 64KB.

## Installation

```bash
go get github.com/meigma/blob
```

Requires Go 1.25 or later.

## Quick Start

```go
import (
    "context"
    "github.com/meigma/blob"
)

ctx := context.Background()

// Push a directory to registry
c, _ := blob.NewClient(blob.WithDockerConfig())
c.Push(ctx, "ghcr.io/org/configs:v1", "./src",
    blob.PushWithCompression(blob.CompressionZstd),
)

// Pull and read files lazily—only downloads what you access
archive, _ := c.Pull(ctx, "ghcr.io/org/configs:v1")
content, _ := archive.ReadFile("config/app.json")
```

## Supply Chain Security

Verify archive provenance with Sigstore signatures and SLSA attestations:

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy"
    "github.com/meigma/blob/policy/sigstore"
    "github.com/meigma/blob/policy/slsa"
)

// Require signatures from GitHub Actions
sigPolicy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main"),
    sigstore.AllowTags("v*"),
)

// Require SLSA provenance from a specific workflow
slsaPolicy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo")

// Pull fails if verification fails
c, _ := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy)),
)
archive, err := c.Pull(ctx, "ghcr.io/org/configs:v1")
```

## Performance

| Metric | Value |
|--------|-------|
| Bandwidth saved | 99.99% (64KB from 1GB archive) |
| Index lookup | 26 ns (constant time) |
| Cache speedup | 43x faster reads |

Path-sorted storage means directories fetch with a single range request. Content-addressed caching deduplicates across archives automatically.

## Features

- **Prove origin** — Sigstore signatures and SLSA attestations
- **Verify on read** — Per-file SHA256 hashes checked automatically
- **Fetch only what you use** — HTTP range requests for individual files
- **Directory fetches** — Single-request reads for entire directories
- **Compression** — Per-file zstd compression preserves random access
- **Caching** — Content-addressed deduplication across archives
- **Standard interfaces** — Implements `fs.FS`, `fs.ReadFileFS`, `fs.ReadDirFS`

## Documentation

- [Getting Started](https://blob.meigma.dev/docs/getting-started) — Complete tutorial
- [Architecture](https://blob.meigma.dev/docs/explanation/architecture) — Design decisions and trade-offs
- [API Reference](https://blob.meigma.dev/docs/reference/api) — Complete API documentation

## License

Licensed under either of [Apache License, Version 2.0](LICENSE-APACHE) or [MIT License](LICENSE-MIT) at your option.
