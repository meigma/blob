# Provenance Example

This example demonstrates end-to-end provenance verification for blob archives, including:

- Creating and pushing archives to OCI registries
- Signing manifests using sigstore (keyless via GitHub OIDC)
- Pulling with policy-based signature verification
- Validating SLSA provenance attestations (optional)
- Verifying source authorization via gittuf (optional)

## Quick Start (Local Testing)

For local testing without signing, use [ttl.sh](https://ttl.sh) (anonymous, temporary storage):

```bash
# Build the CLI
go build -o provenance .

# Push to ttl.sh (1 hour TTL)
./provenance push --ref ttl.sh/my-test-$(date +%s):1h

# Pull without verification (local testing only)
./provenance pull --ref ttl.sh/my-test-...:1h --skip-sig --skip-attest
```

## GitHub Actions Workflow

The included workflow (`.github/workflows/provenance.yml`) demonstrates the full provenance flow:

1. **Build and Push**: Archives assets and pushes to GHCR
2. **Sign**: Creates sigstore signature using `client.Sign()` with keyless signing
3. **Verify**: Pulls with signature policy enforcement

### Workflow Triggers

- Push to `main` branch (on `examples/provenance/**` paths)
- Manual dispatch via `workflow_dispatch`

### Required Permissions

```yaml
permissions:
  contents: read       # Checkout repository
  packages: write      # Push to GHCR
  id-token: write      # OIDC token for keyless signing
```

## Signing Archives

The `--sign` flag uses the blob client's built-in sigstore integration:

```bash
# Push and sign with sigstore (requires OIDC token, e.g., in GitHub Actions)
./provenance push --ref ghcr.io/myorg/archive:v1 --sign
```

This creates a signature using:
- **Fulcio**: Issues short-lived certificates based on OIDC identity
- **Rekor**: Records the signature in the transparency log
- **OCI Referrers**: Attaches signature as an OCI 1.1 referrer artifact

## Manual Verification

After the workflow runs, verify manually:

```bash
# Pull with signature verification
go run . pull \
  --ref ghcr.io/meigma/blob/provenance-example:latest \
  --repo meigma/blob \
  --skip-attest

# Pull from a different repository
go run . pull \
  --ref ghcr.io/myorg/myrepo/archive:v1 \
  --repo myorg/myrepo \
  --skip-attest
```

## Policy Configuration

Verification uses Go-native policies instead of OPA/Rego:

- **Sigstore Policy**: Verifies signatures from GitHub Actions OIDC
- **SLSA Policy**: Validates SLSA provenance attestations (optional)

### Sigstore Verification

The `sigstore.GitHubActionsPolicy()` helper automatically configures:
- GitHub Actions OIDC issuer (`https://token.actions.githubusercontent.com`)
- Repository-scoped subject matching

```go
// Verify signatures from any workflow in the repository
policy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo")

// Restrict to specific branches
policy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main", "release/*"),
)

// Restrict to specific tags
policy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowTags("v*"),
)
```

### Signing Archives Programmatically

Use `client.Sign()` with a sigstore signer:

```go
// Create signer for keyless signing
signer, _ := sigstore.NewSigner(
    sigstore.WithEphemeralKey(),
    sigstore.WithFulcio("https://fulcio.sigstore.dev"),
    sigstore.WithRekor("https://rekor.sigstore.dev"),
    sigstore.WithAmbientCredentials(), // Uses OIDC from environment
)

// Push archive
client, _ := blob.NewClient(blob.WithDockerConfig())
client.Push(ctx, ref, srcDir)

// Sign the manifest (creates OCI 1.1 referrer)
sigDigest, _ := client.Sign(ctx, ref, signer)
```

### SLSA Provenance Verification (Optional)

The `slsa.GitHubActionsWorkflow()` helper validates:
- Build was run by GitHub Actions
- Source repository matches expected value
- Optional: workflow path and ref restrictions

```go
// Verify SLSA provenance from any workflow
policy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo")

// Restrict to specific workflow
policy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowPath(".github/workflows/release.yml"),
)
```

### Gittuf Source Verification (Optional)

The `gittuf.GitHubRepository()` helper verifies that source changes were authorized
according to the repository's gittuf policy. This adds a third layer to the trust chain:

- **Sigstore**: WHO signed the archive
- **SLSA**: HOW the archive was built
- **Gittuf**: WHETHER source changes were authorized

```go
// Verify source authorization for a GitHub repository
policy, _ := gittuf.GitHubRepository("myorg", "myrepo")

// Allow gradual adoption (skip if repo doesn't have gittuf)
policy, _ := gittuf.GitHubRepository("myorg", "myrepo",
    gittuf.WithAllowMissingGittuf(),
)

// Full RSL history verification (slower but more thorough)
policy, _ := gittuf.GitHubRepository("myorg", "myrepo",
    gittuf.WithFullVerification(),
)
```

Gittuf verification works by:
1. Extracting source info (repo, ref, commit) from SLSA provenance
2. Cloning the source repository (cached locally)
3. Verifying the ref against the gittuf Reference State Log (RSL)

For repositories without SLSA provenance, use `WithAllowMissingProvenance()` to
gracefully skip gittuf verification.

### Combining Policies

Use `policy.RequireAll()` to combine multiple policies:

```go
combined := policy.RequireAll(
    sigstorePolicy,  // WHO signed the archive
    slsaPolicy,      // HOW the archive was built (optional)
    gittufPolicy,    // WHETHER source changes were authorized (optional)
)
client, _ := blob.NewClient(blob.WithPolicy(combined))
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         GitHub Actions                               │
├─────────────────────────────────────────────────────────────────────┤
│  1. Build & Push              2. Sign (Sigstore)                     │
│  ┌──────────────┐           ┌──────────────┐                         │
│  │ blob.Create  │──────────▶│ client.Sign  │                         │
│  │ client.Push  │           │ (Fulcio+     │                         │
│  └──────────────┘           │  Rekor)      │                         │
│         │                   └──────────────┘                         │
│         │                          │                                  │
│         ▼                          ▼                                  │
│  ┌────────────────────────────────────────────────────────────┐      │
│  │                    OCI Registry (GHCR)                      │      │
│  │  ┌─────────────┐  ┌─────────────────────────────────────┐  │      │
│  │  │  Manifest   │◀─│ Signature (OCI 1.1 Referrer)        │  │      │
│  │  │  (archive)  │  │ - Sigstore bundle                   │  │      │
│  │  └─────────────┘  │ - Fulcio certificate                │  │      │
│  │        ▲          │ - Rekor inclusion proof             │  │      │
│  │        │          └─────────────────────────────────────┘  │      │
│  └────────┼───────────────────────────────────────────────────┘      │
└───────────┼──────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                           Consumer                                   │
├─────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐      ┌──────────────┐                              │
│  │ client.Pull  │─────▶│ sigstore     │                              │
│  │              │      │ .Policy      │                              │
│  └──────────────┘      └──────────────┘                              │
│         │                     │                                      │
│         │◀── Verification ────┘                                      │
│         ▼        Passed                                              │
│  ┌──────────────┐                                                    │
│  │ blob.CopyDir │  Extract verified files                            │
│  └──────────────┘                                                    │
└─────────────────────────────────────────────────────────────────────┘
```

## Files

| File | Description |
|------|-------------|
| `main.go` | CLI entrypoint with subcommand dispatch |
| `push.go` | Archive creation, push, and signing logic |
| `pull.go` | Pull with policy verification |
| `assets/` | Sample files to archive |

## Dependencies

- `github.com/meigma/blob` - High-level archive client with signing
- `github.com/meigma/blob/policy/sigstore` - Signature verification and signing
- `github.com/meigma/blob/policy/slsa` - SLSA provenance validation (optional)
- `github.com/meigma/blob/policy/gittuf` - Gittuf source verification (optional)
