---
sidebar_position: 3
---

# CLI Getting Started

This tutorial walks through using the `blob` command-line tool to push, inspect, browse, and pull file archives from OCI registries.

> **Library users**: This tutorial covers the CLI. For Go library usage, see [Getting Started](getting-started).

## Prerequisites

- Access to an OCI registry (Docker Hub, ghcr.io, or a local registry)
- Docker configured with registry credentials (optional, for authenticated registries)

## Installation

### Install Script (Recommended)

```bash
curl -sSfL https://blob.meigma.dev/install.sh | sh
```

This installs the latest `blob` binary to `/usr/local/bin`.

### Go Install

```bash
go install github.com/meigma/blob/cmd/blob@latest
```

### Binary Download

Download pre-built binaries from the [releases page](https://github.com/meigma/blob/releases).

### Verify Installation

```bash
blob version
```

## Step 1: Push a Directory

Push a directory to an OCI registry as a blob archive:

```bash
blob push ghcr.io/myorg/myarchive:v1 ./src
```

Add compression for smaller archives:

```bash
blob push --compression=zstd ghcr.io/myorg/myarchive:v1 ./src
```

For local testing without a real registry, run a local registry:

```bash
docker run -d -p 5000:5000 --name registry registry:2

blob push --plain-http localhost:5000/test:v1 ./src
```

> **Note (macOS):** Port 5000 may conflict with AirPlay Receiver. Use port 5001 instead:
> `docker run -d -p 5001:5000 --name registry registry:2`

## Step 2: Inspect Archive Metadata

View archive metadata without downloading the data blob:

```bash
blob inspect ghcr.io/myorg/myarchive:v1
```

Output:

```
Digest:       sha256:abc123...
Created:      2024-01-15T10:30:00Z
Files:        42
Data size:    1.2 MB
Compression:  zstd (ratio: 0.35)
```

List all files in the archive:

```bash
blob ls ghcr.io/myorg/myarchive:v1
```

Output:

```
config.json       1.2 KB
src/main.go       4.5 KB
src/utils/log.go  2.1 KB
...
```

View as a tree:

```bash
blob tree ghcr.io/myorg/myarchive:v1
```

Output:

```
.
├── config.json
└── src/
    ├── main.go
    └── utils/
        └── log.go
```

## Step 3: Browse Interactively

Launch the terminal UI to browse and preview files:

```bash
blob open ghcr.io/myorg/myarchive:v1
```

The TUI provides:
- File tree navigation with arrow keys
- File preview with syntax highlighting
- Search with `/`
- Copy file paths with `y`
- Exit with `q`

## Step 4: Read Files

Print a file to stdout (fetches only that file's bytes via HTTP range request):

```bash
blob cat ghcr.io/myorg/myarchive:v1 config.json
```

Copy specific files to a local directory:

```bash
blob cp ghcr.io/myorg/myarchive:v1:config.json ./local/
blob cp ghcr.io/myorg/myarchive:v1:src/main.go ./local/
```

Copy multiple files:

```bash
blob cp ghcr.io/myorg/myarchive:v1:config.json \
       ghcr.io/myorg/myarchive:v1:src/main.go \
       ./local/
```

## Step 5: Pull Entire Archive

Download and extract the complete archive:

```bash
blob pull ghcr.io/myorg/myarchive:v1 ./dest
```

With options:

```bash
# Overwrite existing files
blob pull --overwrite ghcr.io/myorg/myarchive:v1 ./dest

# Preserve file modes and times
blob pull --preserve-mode --preserve-times ghcr.io/myorg/myarchive:v1 ./dest

# Extract only specific prefix
blob pull --prefix=src ghcr.io/myorg/myarchive:v1 ./dest
```

## Step 6: Configure Aliases and Caching

### Registry Aliases

Create shortcuts for frequently used registries:

```bash
# Add an alias
blob alias add prod ghcr.io/myorg/production

# Use the alias
blob push prod/myarchive:v1 ./src
blob cat prod/myarchive:v1 config.json

# List aliases
blob alias list

# Remove an alias
blob alias rm prod
```

### Caching

Enable caching for faster repeated access:

```bash
# Set cache directory (persists in config)
blob config set cache.dir ~/.cache/blob

# View cache status
blob cache status

# Clear all caches
blob cache clear

# Clear specific cache layer
blob cache clear content
blob cache clear blocks
blob cache clear refs
```

### Configuration File

View and edit configuration:

```bash
# Show current configuration
blob config show

# Set a value
blob config set output json

# Get a value
blob config get cache.dir
```

Configuration is stored in `~/.config/blob/config.yaml` (or `$XDG_CONFIG_HOME/blob/config.yaml`).

Example configuration:

```yaml
output: table
cache:
  dir: ~/.cache/blob
  ref_ttl: 5m
aliases:
  prod: ghcr.io/myorg/production
  staging: ghcr.io/myorg/staging
```

## Next Steps

Now that you have the basics:

- [CLI Reference](reference/cli) - Complete command documentation
- [CLI Workflows](guides/cli-workflows) - Signing, verification, and CI/CD patterns
- [OCI Client](guides/oci-client) - Go library equivalent operations
- [Provenance & Signing](guides/provenance) - Supply chain security details
