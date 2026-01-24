---
sidebar_position: 8
---

# CLI Workflows

Task-oriented guide for common CLI workflows including signing, verification, and CI/CD integration.

## Signing and Verifying Archives

### Keyless Signing (Recommended for CI)

In GitHub Actions or other OIDC-enabled CI environments, use keyless signing:

```bash
# Push and sign in one step
blob push --sign ghcr.io/myorg/archive:v1 ./src

# Or sign separately after pushing
blob push ghcr.io/myorg/archive:v1 ./src
blob sign ghcr.io/myorg/archive:v1
```

Keyless signing uses ambient OIDC credentials from the CI environment. The signature includes the workflow identity, enabling verification that archives came from specific workflows.

### Key-Based Signing

For environments without OIDC, use a private key:

```bash
# Sign with a private key
blob sign --key private.pem ghcr.io/myorg/archive:v1
```

Generate a signing key:

```bash
openssl ecparam -genkey -name prime256v1 -out private.pem
openssl ec -in private.pem -pubout -out public.pem
```

### Verifying Signatures

Verify that an archive was signed by a trusted source:

```bash
# Verify signature from any workflow in the repo
blob verify --repo=myorg/myrepo ghcr.io/myorg/archive:v1

# Verify with branch restriction
blob verify --repo=myorg/myrepo --branches=main ghcr.io/myorg/archive:v1

# Verify with tag restriction
blob verify --repo=myorg/myrepo --tags="v*" ghcr.io/myorg/archive:v1

# Combined restrictions (main branch OR release tags)
blob verify --repo=myorg/myrepo --branches=main --tags="v*" \
    ghcr.io/myorg/archive:v1
```

### Enforcing Verification on Pull

Always verify before extracting:

```bash
# Pull with verification required
blob pull --verify --policy=policy.yaml ghcr.io/myorg/archive:v1 ./dest
```

---

## Policy-Based Verification

### Creating a Policy File

Define verification requirements in a policy file:

```yaml
# policy.yaml - Require GitHub Actions signature from main branch or release tags
signature:
  issuer: https://token.actions.githubusercontent.com
  subject_regex: "^https://github.com/myorg/myrepo/\\.github/workflows/.*@refs/(heads/main|tags/v.*)$"

provenance:
  source_repo: https://github.com/myorg/myrepo
  branches:
    - main
  tags:
    - "v*"
```

### Using Policies

```bash
# Verify with policy
blob verify --policy=policy.yaml ghcr.io/myorg/archive:v1

# Pull with policy enforcement
blob pull --verify --policy=policy.yaml ghcr.io/myorg/archive:v1 ./dest
```

### Policy Examples

**Strict production policy:**

```yaml
# production-policy.yaml
signature:
  issuer: https://token.actions.githubusercontent.com
  # Only release workflow on main branch
  subject_regex: "^https://github.com/myorg/myrepo/\\.github/workflows/release\\.yml@refs/heads/main$"

provenance:
  builder: https://github.com/slsa-framework/slsa-github-generator
  source_repo: https://github.com/myorg/myrepo
  branches:
    - main
```

**Multi-repo policy:**

```yaml
# team-policy.yaml
signature:
  issuer: https://token.actions.githubusercontent.com
  # Allow any repo in the org
  subject_regex: "^https://github.com/myorg/.*"

provenance:
  source_repo_regex: "^https://github.com/myorg/.*"
  tags:
    - "v*"
```

---

## CI/CD Integration

### GitHub Actions: Build and Sign

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: read
  packages: write
  id-token: write  # Required for keyless signing

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install blob CLI
        run: curl -sSfL https://blob.meigma.dev/install.sh | sh

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build assets
        run: |
          mkdir -p dist
          # Your build steps here
          cp -r ./assets ./dist/

      - name: Push and sign archive
        run: |
          blob push --sign --compression=zstd \
            -t latest \
            -a "org.opencontainers.image.source=https://github.com/${{ github.repository }}" \
            -a "org.opencontainers.image.revision=${{ github.sha }}" \
            ghcr.io/${{ github.repository }}/assets:${{ github.ref_name }} \
            ./dist
```

### GitHub Actions: Verified Pull

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to deploy'
        required: true

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Install blob CLI
        run: curl -sSfL https://blob.meigma.dev/install.sh | sh

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Pull with verification
        run: |
          blob pull --verify \
            --repo=${{ github.repository }} \
            --branches=main \
            --tags="v*" \
            ghcr.io/${{ github.repository }}/assets:${{ inputs.version }} \
            ./deploy

      - name: Deploy
        run: |
          # Your deployment steps
          rsync -av ./deploy/ /var/www/app/
```

### GitLab CI: Build and Sign

```yaml
# .gitlab-ci.yml
stages:
  - build
  - release

build:
  stage: build
  script:
    - mkdir -p dist
    - # Your build steps
  artifacts:
    paths:
      - dist/

release:
  stage: release
  image: golang:1.21
  id_tokens:
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  script:
    - curl -sSfL https://blob.meigma.dev/install.sh | sh
    - blob push --sign --compression=zstd
        registry.gitlab.com/$CI_PROJECT_PATH/assets:$CI_COMMIT_TAG
        ./dist
  rules:
    - if: $CI_COMMIT_TAG
```

### Using with Docker Compose

```yaml
# docker-compose.yml
services:
  app:
    build: .
    volumes:
      - assets:/app/assets

  assets-sync:
    image: alpine
    command: |
      sh -c "
        curl -sSfL https://blob.meigma.dev/install.sh | sh
        blob pull --verify --repo=myorg/myrepo \
          ghcr.io/myorg/assets:latest /app/assets
      "
    volumes:
      - assets:/app/assets

volumes:
  assets:
```

---

## Using Aliases for Efficiency

### Setting Up Aliases

```bash
# Production registry
blob alias add prod ghcr.io/myorg/production

# Staging registry
blob alias add staging ghcr.io/myorg/staging

# Development (local registry)
blob alias add dev localhost:5000/dev
```

### Using Aliases

```bash
# Push to production
blob push prod/assets:v1.0.0 ./dist

# Pull from staging
blob pull staging/assets:latest ./deploy

# Compare versions
blob inspect prod/assets:v1.0.0
blob inspect staging/assets:v1.0.0
```

### Managing Aliases

```bash
# List all aliases
blob alias list

# Remove an alias
blob alias rm dev

# Aliases in config file
blob config show
```

---

## Cache Management

### Enabling Caching

```bash
# Enable caching (one-time setup)
blob config set cache.dir ~/.cache/blob
```

### Monitoring Cache

```bash
# View cache statistics
blob cache status

# Example output:
# CACHE     SIZE      MAX       ENTRIES
# refs      1.2 KB    5 MB      15
# manifests 45 KB     10 MB     12
# indexes   2.1 MB    50 MB     8
# content   89 MB     100 MB    1,247
# blocks    12 MB     50 MB     892
```

### Clearing Caches

```bash
# Clear all caches
blob cache clear

# Clear specific cache layer
blob cache clear content   # File content cache
blob cache clear blocks    # HTTP range block cache
blob cache clear refs      # Tag resolution cache
blob cache clear manifests # Manifest cache
blob cache clear indexes   # Index blob cache
```

### Cache Configuration

```bash
# Set reference cache TTL (for mutable tags like 'latest')
blob config set cache.ref_ttl 5m

# Set cache size limits
blob config set cache.content_max 500M
blob config set cache.blocks_max 100M
```

### CI/CD Caching

In CI environments, cache the blob cache directory:

**GitHub Actions:**

```yaml
- uses: actions/cache@v4
  with:
    path: ~/.cache/blob
    key: blob-cache-${{ runner.os }}
    restore-keys: |
      blob-cache-
```

**GitLab CI:**

```yaml
cache:
  paths:
    - .cache/blob/

variables:
  BLOB_CACHE_DIR: .cache/blob
```

---

## Scripting with JSON Output

Use JSON output for scripting and automation:

```bash
# Get archive info as JSON
blob inspect -o json ghcr.io/myorg/archive:v1 | jq '.digest'

# List files as JSON
blob ls -o json ghcr.io/myorg/archive:v1 | jq '.[].path'

# Check file count
blob inspect -o json ghcr.io/myorg/archive:v1 | jq '.file_count'

# Get specific file sizes
blob ls -o json ghcr.io/myorg/archive:v1 | \
  jq -r '.[] | select(.path | startswith("src/")) | "\(.path): \(.size)"'
```

### Example: Diff Two Archives

```bash
#!/bin/bash
# diff-archives.sh - Compare file lists between two archive versions

OLD=$1
NEW=$2

echo "Files only in $OLD:"
comm -23 \
  <(blob ls -o json "$OLD" | jq -r '.[].path' | sort) \
  <(blob ls -o json "$NEW" | jq -r '.[].path' | sort)

echo ""
echo "Files only in $NEW:"
comm -13 \
  <(blob ls -o json "$OLD" | jq -r '.[].path' | sort) \
  <(blob ls -o json "$NEW" | jq -r '.[].path' | sort)
```

Usage:

```bash
./diff-archives.sh ghcr.io/myorg/archive:v1.0.0 ghcr.io/myorg/archive:v1.1.0
```

---

## See Also

- [CLI Reference](../reference/cli) - Complete command documentation
- [CLI Getting Started](../cli-getting-started) - Tutorial introduction
- [Provenance & Signing](provenance) - Detailed signing and verification concepts
- [Caching](caching) - Cache architecture and configuration
