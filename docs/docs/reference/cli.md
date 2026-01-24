---
sidebar_position: 2
---

# CLI Reference

Complete reference for the `blob` command-line tool.

## Global Flags

These flags apply to all commands:

| Flag | Description | Default |
|------|-------------|---------|
| `--config` | Path to config file | `~/.config/blob/config.yaml` |
| `--output`, `-o` | Output format: `table`, `json`, `plain` | `table` |
| `-v`, `--verbose` | Enable verbose output | false |
| `-q`, `--quiet` | Suppress non-essential output | false |
| `--no-color` | Disable colored output | false |
| `--plain-http` | Use HTTP instead of HTTPS for registries | false |

---

## Core Commands

### blob push

Push a directory to an OCI registry as a blob archive.

**Synopsis:**

```
blob push [flags] <reference> <directory>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference with tag (e.g., `ghcr.io/org/repo:v1`) |
| `directory` | Source directory to archive |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--compression`, `-c` | Compression: `none`, `zstd` | `none` |
| `--tag`, `-t` | Additional tags to apply (repeatable) | |
| `--annotation`, `-a` | Add annotation `key=value` (repeatable) | |
| `--max-files` | Maximum file count (0 = unlimited) | 200000 |
| `--sign` | Sign the archive after pushing | false |

**Examples:**

```bash
# Basic push
blob push ghcr.io/myorg/archive:v1 ./src

# With compression
blob push --compression=zstd ghcr.io/myorg/archive:v1 ./src

# Multiple tags
blob push -t latest -t stable ghcr.io/myorg/archive:v1.0.0 ./src

# With annotations
blob push -a "org.opencontainers.image.source=https://github.com/myorg/repo" \
          ghcr.io/myorg/archive:v1 ./src

# Push and sign (keyless, requires OIDC environment)
blob push --sign ghcr.io/myorg/archive:v1 ./src
```

**See Also:** [OCI Client - Push Operations](../guides/oci-client#push-operations)

---

### blob pull

Pull an archive from a registry and extract to a directory.

**Synopsis:**

```
blob pull [flags] <reference> <directory>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference (e.g., `ghcr.io/org/repo:v1`) |
| `directory` | Destination directory for extraction |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--prefix`, `-p` | Extract only files under this prefix | `.` (all) |
| `--overwrite` | Overwrite existing files | false |
| `--preserve-mode` | Preserve file permission modes | false |
| `--preserve-times` | Preserve file modification times | false |
| `--clean` | Remove destination before extracting | false |
| `--workers` | Parallel extraction workers (0 = auto) | 0 |
| `--skip-cache` | Bypass all caches | false |
| `--verify` | Require signature verification | false |
| `--policy` | Path to verification policy file | |

**Examples:**

```bash
# Basic pull
blob pull ghcr.io/myorg/archive:v1 ./dest

# Extract specific directory
blob pull --prefix=config ghcr.io/myorg/archive:v1 ./dest

# With metadata preservation
blob pull --preserve-mode --preserve-times ghcr.io/myorg/archive:v1 ./dest

# Clean extraction (removes existing files)
blob pull --clean ghcr.io/myorg/archive:v1 ./dest

# With signature verification
blob pull --verify ghcr.io/myorg/archive:v1 ./dest
```

**See Also:** [Extracting Files](../guides/extraction)

---

### blob cp

Copy specific files from an archive to a local directory.

**Synopsis:**

```
blob cp [flags] <source>... <directory>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `source` | Archive file reference: `<ref>:<path>` |
| `directory` | Destination directory |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--overwrite` | Overwrite existing files | false |
| `--preserve-mode` | Preserve file permission modes | false |
| `--preserve-times` | Preserve file modification times | false |

**Examples:**

```bash
# Copy single file
blob cp ghcr.io/myorg/archive:v1:config.json ./local/

# Copy multiple files
blob cp ghcr.io/myorg/archive:v1:config.json \
        ghcr.io/myorg/archive:v1:src/main.go \
        ./local/

# With overwrite
blob cp --overwrite ghcr.io/myorg/archive:v1:config.json ./local/
```

---

### blob cat

Print file contents to stdout.

**Synopsis:**

```
blob cat [flags] <reference> <path>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference |
| `path` | File path within the archive |

**Examples:**

```bash
# Print file contents
blob cat ghcr.io/myorg/archive:v1 config.json

# Pipe to other commands
blob cat ghcr.io/myorg/archive:v1 data.json | jq '.items[]'
```

---

## Inspection Commands

### blob ls

List files in an archive.

**Synopsis:**

```
blob ls [flags] <reference> [prefix]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference |
| `prefix` | Optional path prefix to filter files |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--long`, `-l` | Show detailed file information | false |
| `--human`, `-h` | Human-readable sizes | false |
| `--sort` | Sort by: `name`, `size`, `time` | `name` |
| `--reverse`, `-r` | Reverse sort order | false |

**Examples:**

```bash
# List all files
blob ls ghcr.io/myorg/archive:v1

# List with details
blob ls -lh ghcr.io/myorg/archive:v1

# List specific directory
blob ls ghcr.io/myorg/archive:v1 src/

# Sort by size
blob ls -l --sort=size ghcr.io/myorg/archive:v1
```

---

### blob tree

Display archive contents as a tree.

**Synopsis:**

```
blob tree [flags] <reference> [prefix]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference |
| `prefix` | Optional path prefix |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--depth`, `-d` | Maximum tree depth (0 = unlimited) | 0 |
| `--dirs-only` | Show only directories | false |

**Examples:**

```bash
# Full tree
blob tree ghcr.io/myorg/archive:v1

# Limited depth
blob tree -d 2 ghcr.io/myorg/archive:v1

# Specific subtree
blob tree ghcr.io/myorg/archive:v1 src/
```

---

### blob inspect

Show archive metadata and statistics.

**Synopsis:**

```
blob inspect [flags] <reference>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--referrers` | List referrer artifacts (signatures, attestations) | false |
| `--skip-cache` | Bypass all caches | false |

**Examples:**

```bash
# Basic inspection
blob inspect ghcr.io/myorg/archive:v1

# Include referrers (signatures, attestations)
blob inspect --referrers ghcr.io/myorg/archive:v1
```

**Output (table format):**

```
Digest:             sha256:abc123...
Created:            2024-01-15T10:30:00Z
Files:              42
Data blob size:     1.2 MB
Index blob size:    8.5 KB
Uncompressed size:  3.4 MB
Compression ratio:  0.35

Annotations:
  org.opencontainers.image.source  https://github.com/myorg/repo
  org.opencontainers.image.created 2024-01-15T10:30:00Z
```

**See Also:** [OCI Client - Inspect Operations](../guides/oci-client#inspect-operations-metadata--file-index)

---

### blob open

Open interactive TUI file browser.

**Synopsis:**

```
blob open [flags] <reference>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference |

**Keyboard Shortcuts:**

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate files |
| `Enter` | Open file / expand directory |
| `h` or `←` | Go to parent directory |
| `l` or `→` | Expand / preview |
| `/` | Search |
| `n/N` | Next / previous search result |
| `y` | Copy file path |
| `q` | Quit |

**Examples:**

```bash
# Browse archive
blob open ghcr.io/myorg/archive:v1
```

---

## Security Commands

### blob sign

Sign an archive manifest with Sigstore.

**Synopsis:**

```
blob sign [flags] <reference>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference (must exist) |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--key` | Path to private key PEM file | (keyless) |
| `--fulcio` | Fulcio server URL | `https://fulcio.sigstore.dev` |
| `--rekor` | Rekor transparency log URL | `https://rekor.sigstore.dev` |
| `--oidc-issuer` | OIDC issuer URL | (auto-detect) |
| `--oidc-token` | OIDC identity token | (ambient) |

**Examples:**

```bash
# Keyless signing (GitHub Actions with OIDC)
blob sign ghcr.io/myorg/archive:v1

# With private key
blob sign --key private.pem ghcr.io/myorg/archive:v1
```

**See Also:** [Provenance & Signing - Signing Archives](../guides/provenance#signing-archives)

---

### blob verify

Verify archive signature and attestations.

**Synopsis:**

```
blob verify [flags] <reference>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | OCI reference |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--policy` | Path to policy YAML file | |
| `--issuer` | Required OIDC issuer | |
| `--identity` | Required signer identity (subject) | |
| `--repo` | GitHub repo for GitHub Actions policy (owner/repo) | |
| `--branches` | Allowed branches (comma-separated, supports wildcards) | |
| `--tags` | Allowed tags (comma-separated, supports wildcards) | |

**Examples:**

```bash
# Verify with GitHub Actions policy
blob verify --repo=myorg/myrepo ghcr.io/myorg/archive:v1

# Verify with branch/tag restrictions
blob verify --repo=myorg/myrepo --branches=main --tags="v*" \
    ghcr.io/myorg/archive:v1

# Verify with policy file
blob verify --policy=policy.yaml ghcr.io/myorg/archive:v1

# Verify with explicit identity
blob verify --issuer="https://token.actions.githubusercontent.com" \
            --identity="https://github.com/myorg/repo/.github/workflows/release.yml@refs/heads/main" \
            ghcr.io/myorg/archive:v1
```

**Policy File Format:**

```yaml
# policy.yaml
signature:
  issuer: https://token.actions.githubusercontent.com
  subject_regex: "^https://github.com/myorg/.*"

provenance:
  builder: https://github.com/slsa-framework/slsa-github-generator
  source_repo: https://github.com/myorg/myrepo
  branches:
    - main
  tags:
    - "v*"
```

**See Also:** [Provenance & Signing - Verification](../guides/provenance#sigstore-signature-verification)

---

### blob tag

Create or update a tag pointing to an existing manifest.

**Synopsis:**

```
blob tag [flags] <reference> <new-tag>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `reference` | Source reference (tag or digest) |
| `new-tag` | New tag name |

**Examples:**

```bash
# Tag v1.0.0 as latest
blob tag ghcr.io/myorg/archive:v1.0.0 latest

# Tag by digest
blob tag ghcr.io/myorg/archive@sha256:abc123... stable
```

---

## Management Commands

### blob alias

Manage registry aliases.

**Synopsis:**

```
blob alias <subcommand>
```

**Subcommands:**

#### blob alias add

```
blob alias add <name> <registry-prefix>
```

**Examples:**

```bash
blob alias add prod ghcr.io/myorg/production
blob alias add staging ghcr.io/myorg/staging
```

#### blob alias rm

```
blob alias rm <name>
```

#### blob alias list

```
blob alias list
```

**Output:**

```
NAME     REGISTRY PREFIX
prod     ghcr.io/myorg/production
staging  ghcr.io/myorg/staging
```

---

### blob cache

Manage local caches.

**Synopsis:**

```
blob cache <subcommand>
```

**Subcommands:**

#### blob cache status

Show cache statistics.

```
blob cache status
```

**Output:**

```
CACHE     SIZE      MAX       ENTRIES
refs      1.2 KB    5 MB      15
manifests 45 KB     10 MB     12
indexes   2.1 MB    50 MB     8
content   89 MB     100 MB    1,247
blocks    12 MB     50 MB     892
```

#### blob cache clear

Clear caches.

```
blob cache clear [layer]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `layer` | Optional: `refs`, `manifests`, `indexes`, `content`, `blocks` |

**Examples:**

```bash
# Clear all caches
blob cache clear

# Clear only content cache
blob cache clear content
```

---

### blob config

Manage configuration.

**Synopsis:**

```
blob config <subcommand>
```

**Subcommands:**

#### blob config show

Display current configuration.

```
blob config show
```

#### blob config get

Get a configuration value.

```
blob config get <key>
```

#### blob config set

Set a configuration value.

```
blob config set <key> <value>
```

**Examples:**

```bash
blob config set output json
blob config set cache.dir ~/.cache/blob
blob config set cache.ref_ttl 10m
```

---

### blob version

Print version information.

**Synopsis:**

```
blob version [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--short` | Print version number only |

**Output:**

```
blob version 1.0.0
  commit: abc1234
  built:  2024-01-15T10:30:00Z
  go:     go1.21.5
```

---

## Configuration

### Config File Format

The configuration file is located at `~/.config/blob/config.yaml` (or `$XDG_CONFIG_HOME/blob/config.yaml`).

```yaml
# Output format: table, json, plain
output: table

# Disable colored output
no_color: false

# Use HTTP for all registries
plain_http: false

# Cache configuration
cache:
  # Cache directory (empty = caching disabled)
  dir: ~/.cache/blob

  # Reference cache TTL
  ref_ttl: 5m

  # Cache size limits (bytes, supports K/M/G suffixes)
  content_max: 100M
  blocks_max: 50M
  refs_max: 5M
  manifests_max: 10M
  indexes_max: 50M

# Registry aliases
aliases:
  prod: ghcr.io/myorg/production
  staging: ghcr.io/myorg/staging

# Default verification policy
verify:
  enabled: false
  policy: ~/.config/blob/policy.yaml
```

### Environment Variables

| Variable | Description | Equivalent Flag |
|----------|-------------|-----------------|
| `BLOB_CONFIG` | Config file path | `--config` |
| `BLOB_OUTPUT` | Output format | `--output` |
| `BLOB_CACHE_DIR` | Cache directory | `cache.dir` |
| `BLOB_NO_COLOR` | Disable colors (any value) | `--no-color` |
| `BLOB_PLAIN_HTTP` | Use HTTP (any value) | `--plain-http` |

Environment variables override config file values. Flags override both.

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments or usage |
| 3 | Archive not found |
| 4 | Authentication failed |
| 5 | Network error |
| 6 | Verification failed (signature or policy) |
| 7 | Hash mismatch (integrity error) |

---

## See Also

- [CLI Getting Started](../cli-getting-started) - Tutorial for CLI usage
- [CLI Workflows](../guides/cli-workflows) - CI/CD and advanced workflows
- [API Reference](api) - Go library reference
