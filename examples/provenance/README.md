# Provenance Example

This example demonstrates end-to-end provenance verification for blob archives, including:

- Creating and pushing archives to OCI registries
- Signing with Sigstore (keyless via GitHub OIDC)
- Attaching SLSA provenance attestations
- Pulling with policy-based verification

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
2. **Attest**: Attaches SLSA provenance via `actions/attest-build-provenance`
3. **Verify**: Pulls with full policy enforcement

### Workflow Triggers

- Push to `main` branch (on `examples/provenance/**` paths)
- Manual dispatch via `workflow_dispatch`

### Required Permissions

```yaml
permissions:
  contents: read       # Checkout repository
  packages: write      # Push to GHCR
  id-token: write      # OIDC token for keyless signing
  attestations: write  # Attach attestations
```

## Manual Verification

After the workflow runs, verify manually:

```bash
# Pull with full verification (signature + SLSA provenance)
go run . pull \
  --ref ghcr.io/meigma/blob/provenance-example:latest \
  --repo meigma/blob

# Pull from a different repository
go run . pull \
  --ref ghcr.io/myorg/myrepo/archive:v1 \
  --repo myorg/myrepo
```

## Policy Configuration

Verification uses Go-native policies instead of OPA/Rego:

- **Sigstore Policy**: Verifies signatures from GitHub Actions OIDC
- **SLSA Policy**: Validates SLSA provenance attestations

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

### SLSA Provenance Verification

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

// Restrict to specific branches/tags
policy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowBranches("main"),
    slsa.WithWorkflowTags("v*"),
)
```

### Combining Policies

Use `policy.RequireAll()` to combine multiple policies:

```go
combined := policy.RequireAll(
    sigstorePolicy,  // Signature verification
    slsaPolicy,      // Provenance verification
)
client, _ := blob.NewClient(blob.WithPolicy(combined))
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         GitHub Actions                               │
├─────────────────────────────────────────────────────────────────────┤
│  1. Build Archive           2. Attest (SLSA)                         │
│  ┌──────────────┐           ┌──────────────┐                         │
│  │ blob.Create  │──────────▶│ attest-build │                         │
│  │ client.Push  │           │ -provenance  │                         │
│  └──────────────┘           └──────────────┘                         │
│         │                          │                                  │
│         ▼                          ▼                                  │
│  ┌────────────────────────────────────────────────────────────┐      │
│  │                    OCI Registry (GHCR)                      │      │
│  │  ┌─────────────┐  ┌─────────────────────────────────────┐  │      │
│  │  │  Manifest   │  │ SLSA Attestation (Sigstore Bundle)  │  │      │
│  │  │  (archive)  │◀─│ - SLSA provenance                   │  │      │
│  │  └─────────────┘  │ - Sigstore signature                │  │      │
│  │        ▲          └─────────────────────────────────────┘  │      │
│  └────────┼───────────────────────────────────────────────────┘      │
└───────────┼──────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                           Consumer                                   │
├─────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐      ┌──────────────┐     ┌──────────────┐        │
│  │ client.Pull  │─────▶│ sigstore     │────▶│ slsa.Policy  │        │
│  │              │      │ .Policy      │     │ (Go-native)  │        │
│  └──────────────┘      └──────────────┘     └──────────────┘        │
│         │                                          │                 │
│         │◀─────────── Verification Passed ─────────┘                 │
│         ▼                                                            │
│  ┌──────────────┐                                                    │
│  │ blob.CopyDir │  Extract verified files                            │
│  └──────────────┘                                                    │
└─────────────────────────────────────────────────────────────────────┘
```

## Files

| File | Description |
|------|-------------|
| `main.go` | CLI entrypoint with subcommand dispatch |
| `push.go` | Archive creation and push logic |
| `pull.go` | Pull with policy verification |
| `assets/` | Sample files to archive |

## Dependencies

- `github.com/meigma/blob` - High-level archive client
- `github.com/meigma/blob/policy/sigstore` - Signature verification
- `github.com/meigma/blob/policy/slsa` - SLSA provenance validation

External tools (CI only):
- [actions/attest-build-provenance](https://github.com/actions/attest-build-provenance) - SLSA attestation with Sigstore signing
