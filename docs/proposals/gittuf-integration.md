# Proposal: Gittuf Integration for Source Provenance

**Status:** Proposal
**Author:** Claude
**Date:** 2026-01-20

## Executive Summary

This proposal explores integrating [gittuf](https://gittuf.dev/) with Blob to provide **source provenance** verification. While Blob currently offers robust build provenance through SLSA attestations (proving *how* an archive was built), it lacks verification of *what* authorized the source code changes. Gittuf fills this gap by providing cryptographic proof that Git repository changes followed security policies.

**Recommendation:** Integration is **feasible and valuable**. Gittuf's Go implementation, TUF-based trust model, and well-defined verification semantics align well with Blob's existing policy framework.

## Background

### Current Blob Security Model

Blob provides a comprehensive integrity chain:

```
Sigstore Signature → OCI Manifest → Index Digest → Per-file SHA256 Hashes
                                  → Data Digest
```

Current provenance capabilities:
- **Sigstore signatures**: Keyless signing using OIDC (e.g., GitHub Actions)
- **SLSA attestations**: Build provenance describing builder, source repo, and workflow
- **Policy composition**: `RequireAll`/`RequireAny` for complex verification rules

**The Gap:** SLSA provenance records *which* Git commit was built, but not *whether that commit was authorized*. A compromised CI system could build and sign malicious code that appears legitimate.

### What Gittuf Provides

Gittuf is a platform-agnostic Git security layer (OpenSSF incubating project) that:

1. **Reference State Log (RSL)**: Authenticated, tamper-evident log of all Git ref changes stored at `refs/gittuf/reference-state-log`
2. **Policy metadata**: TUF-based delegation model stored at `refs/gittuf/policy`
3. **Attestations**: In-toto attestations for code review approvals at `refs/gittuf/attestations`
4. **Independent verification**: Anyone can verify policy compliance without trusting the forge

Key security properties:
- Removes the Git forge as a single point of trust
- Cryptographic proof that changes were made by authorized parties
- Support for threshold signatures and delegation hierarchies
- Protection against retroactive history modification

## Integration Architecture

### Proposed Design

Create a new `policy/gittuf` package that implements `registry.Policy`:

```go
// policy/gittuf/policy.go
package gittuf

import (
    "context"
    "github.com/meigma/blob/registry"
)

// Policy implements registry.Policy for gittuf verification.
type Policy struct {
    // Repository URL to verify against
    repoURL string

    // Expected policy root key fingerprints
    rootKeys []string

    // Optional: specific namespaces (branches/paths) to verify
    namespaces []string

    // gittuf verification options
    verifyOpts []VerifyOption
}

// Evaluate implements registry.Policy.
func (p *Policy) Evaluate(ctx context.Context, req registry.PolicyRequest) error {
    // 1. Extract source commit from SLSA provenance or attestation
    commit, err := extractSourceCommit(ctx, req)
    if err != nil {
        return fmt.Errorf("gittuf: %w", err)
    }

    // 2. Fetch gittuf metadata from source repository
    gittufMeta, err := fetchGittufMetadata(ctx, p.repoURL, commit)
    if err != nil {
        return fmt.Errorf("gittuf: failed to fetch metadata: %w", err)
    }

    // 3. Verify RSL entries for the commit
    if err := verifyRSL(ctx, gittufMeta, commit, p.rootKeys); err != nil {
        return fmt.Errorf("gittuf: RSL verification failed: %w", err)
    }

    // 4. Verify policy compliance for changed files
    if err := verifyPolicy(ctx, gittufMeta, commit, p.namespaces); err != nil {
        return fmt.Errorf("gittuf: policy verification failed: %w", err)
    }

    return nil
}
```

### Usage Example

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy"
    "github.com/meigma/blob/policy/gittuf"
    "github.com/meigma/blob/policy/sigstore"
    "github.com/meigma/blob/policy/slsa"
)

// Complete provenance chain: signature + build + source
sigPolicy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo")
slsaPolicy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo")
gittufPolicy, _ := gittuf.NewPolicy(
    gittuf.WithRepository("https://github.com/myorg/myrepo"),
    gittuf.WithRootKeys("SHA256:abc123...", "SHA256:def456..."),
    gittuf.ProtectBranch("main"),
)

c, _ := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy, gittufPolicy)),
)

// Pull verifies: signature → build provenance → source authorization
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
```

### Integration Points

#### 1. Source Commit Extraction

The gittuf policy needs to know which Git commit to verify. This can be extracted from:

- **SLSA provenance**: `predicate.buildDefinition.resolvedDependencies[].digest.gitCommit`
- **Custom attestation**: A new "source attestation" type linking archive to commit
- **Manifest annotation**: OCI annotation with commit SHA

Recommended approach: Extract from existing SLSA provenance to avoid new attestation types.

#### 2. Gittuf Metadata Fetching

Options for accessing gittuf metadata:

| Approach | Pros | Cons |
|----------|------|------|
| **Clone repository** | Complete verification | Slow, requires full clone |
| **Git protocol fetch** | Efficient, fetch only `refs/gittuf/*` | Requires Git protocol access |
| **GitHub API** | Works through firewalls | GitHub-specific, rate limits |
| **Bundled attestation** | Self-contained, fast | Increases artifact size |

**Recommendation:** Support multiple backends with Git protocol fetch as primary.

#### 3. Verification Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Blob Pull Request                           │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 1. Sigstore Policy: Verify manifest signature                       │
│    └─ Confirms WHO built the archive                                │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 2. SLSA Policy: Verify build provenance                             │
│    └─ Confirms HOW the archive was built (builder, workflow)        │
│    └─ Extracts source commit SHA                                    │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 3. Gittuf Policy: Verify source authorization                       │
│    ├─ Fetch refs/gittuf/* from source repository                    │
│    ├─ Validate policy root of trust                                 │
│    ├─ Verify RSL entries for the commit                             │
│    └─ Confirm authorized signers approved the changes               │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Archive verified: signature + build + source provenance             │
└─────────────────────────────────────────────────────────────────────┘
```

## Technical Implementation

### Dependencies

The gittuf project is written in Go with well-structured internal packages:

```
github.com/gittuf/gittuf/
├── internal/
│   ├── policy/      # Policy definitions and enforcement
│   ├── rsl/         # Reference State Log implementation
│   ├── tuf/         # TUF metadata handling
│   ├── signerverifier/  # Cryptographic operations
│   └── gitinterface/    # Git operations
```

**Challenge:** Gittuf's packages are in `internal/`, not exported for external use.

**Solutions:**

1. **Upstream contribution**: Work with gittuf maintainers to export a verification API
2. **Fork/vendor**: Include necessary code (Apache-2.0 licensed)
3. **CLI wrapper**: Shell out to `gittuf verify-ref` command
4. **Collaborate on SDK**: Partner to create `github.com/gittuf/gittuf-go` library

**Recommendation:** Approach gittuf maintainers about exporting a verification SDK. This benefits both projects and the broader supply chain security ecosystem.

### Proposed API

```go
package gittuf

// NewPolicy creates a gittuf source provenance policy.
func NewPolicy(opts ...PolicyOption) (*Policy, error)

// PolicyOption configures the policy.
type PolicyOption func(*Policy) error

// WithRepository sets the source repository to verify against.
func WithRepository(url string) PolicyOption

// WithRootKeys sets the trusted root key fingerprints.
// At least one root key must be provided.
func WithRootKeys(fingerprints ...string) PolicyOption

// ProtectBranch requires verification for changes to specific branches.
func ProtectBranch(patterns ...string) PolicyOption

// ProtectPath requires verification for changes to specific paths.
func ProtectPath(patterns ...string) PolicyOption

// WithGitFetcher sets a custom Git metadata fetcher.
func WithGitFetcher(fetcher GitFetcher) PolicyOption

// RequireThreshold requires N-of-M signatures for changes.
func RequireThreshold(n int) PolicyOption

// GitFetcher abstracts gittuf metadata retrieval.
type GitFetcher interface {
    // FetchRefs fetches gittuf refs from a repository.
    FetchRefs(ctx context.Context, repoURL string) (*GittufMetadata, error)
}

// --- Convenience constructors ---

// GitHubRepository creates a policy for a GitHub-hosted repository.
func GitHubRepository(owner, repo string, opts ...PolicyOption) (*Policy, error)

// WithTrustOnFirstUse enables TOFU for root key discovery.
// WARNING: Only use for development/testing.
func WithTrustOnFirstUse() PolicyOption
```

### Error Types

```go
var (
    // ErrNoSourceCommit indicates the source commit couldn't be determined.
    ErrNoSourceCommit = errors.New("gittuf: no source commit in provenance")

    // ErrRSLNotFound indicates the repository lacks gittuf metadata.
    ErrRSLNotFound = errors.New("gittuf: no RSL found in repository")

    // ErrUntrustedRootKey indicates the policy's root keys don't match.
    ErrUntrustedRootKey = errors.New("gittuf: root key not in trusted set")

    // ErrPolicyViolation indicates changes weren't authorized.
    ErrPolicyViolation = errors.New("gittuf: unauthorized changes detected")

    // ErrRSLGap indicates missing RSL entries.
    ErrRSLGap = errors.New("gittuf: gap detected in RSL")
)
```

## Security Considerations

### Trust Model

The integration creates a trust chain:

```
Blob Consumer
    │
    ├─── trusts ──→ OCI Registry (manifest storage)
    │
    ├─── trusts ──→ Sigstore (signature verification)
    │
    ├─── trusts ──→ SLSA Builder (build provenance)
    │
    └─── trusts ──→ Gittuf Root Keys (source authorization)
                         │
                         └─── delegates to ──→ Developer Keys
```

**Key security properties:**
- Compromised CI cannot forge source authorization (gittuf verified independently)
- Compromised developer key is limited by gittuf policy delegation
- Forge compromise doesn't affect verification (gittuf metadata is in-repo)

### Attack Scenarios Addressed

| Attack | Without Gittuf | With Gittuf |
|--------|----------------|-------------|
| Malicious PR merged by compromised maintainer | ✗ Undetected | ✓ Detectable via policy |
| Force push to protected branch | ✗ Undetected | ✓ RSL shows unauthorized change |
| CI compromise building unauthorized commit | ✗ Valid SLSA | ✓ Gittuf fails verification |
| Retroactive history rewrite | ✗ Possible | ✓ RSL hash chain prevents |

### Limitations

1. **Adoption dependency**: Source repositories must have gittuf enabled
2. **Root key bootstrapping**: Consumers need out-of-band root key verification
3. **Performance**: Additional verification step adds latency
4. **Partial coverage**: Only verifies gittuf-protected namespaces

## Implementation Phases

### Phase 1: Core Integration (MVP)

- [ ] Create `policy/gittuf` package structure
- [ ] Implement basic verification against local gittuf metadata
- [ ] Add Git protocol fetcher for `refs/gittuf/*`
- [ ] Support extraction of source commit from SLSA provenance
- [ ] Add comprehensive tests with gittuf test repositories

### Phase 2: Production Hardening

- [ ] Add caching for gittuf metadata (with TTL)
- [ ] Implement GitHub API fallback fetcher
- [ ] Add metrics and observability hooks
- [ ] Create troubleshooting guide and error messages
- [ ] Performance optimization (parallel fetching)

### Phase 3: Advanced Features

- [ ] Support for bundled gittuf attestations (self-contained verification)
- [ ] Threshold signature requirements in policy
- [ ] Path-specific policies (e.g., stricter rules for `/security/*`)
- [ ] Integration with gittuf attestations for code review verification

### Phase 4: Ecosystem

- [ ] GitHub Action for creating gittuf-aware archives
- [ ] Documentation and tutorials
- [ ] Example workflows for common scenarios
- [ ] Upstream collaboration on gittuf Go SDK

## Alternatives Considered

### 1. Git Commit Signatures Only

**Approach:** Verify GPG/SSH signatures on Git commits.

**Why not:**
- No key management or delegation
- Can't enforce policies (who can sign what)
- Signatures optional per-commit

### 2. GitHub Branch Protection Attestation

**Approach:** Trust GitHub's attestation that branch protection was enforced.

**Why not:**
- GitHub becomes single point of trust
- Not portable to other forges
- Can't independently verify

### 3. In-toto Layout Policies

**Approach:** Use in-toto for source verification.

**Why not:**
- Designed for build pipelines, not Git workflows
- Requires separate infrastructure
- Gittuf is purpose-built for Git

### 4. Custom Source Attestation

**Approach:** Build our own source provenance attestation format.

**Why not:**
- Duplicates gittuf's work
- Fragmented ecosystem
- Less mature security analysis

## Open Questions

1. **SDK availability**: Will gittuf export a verification SDK, or should we vendor/wrap?
2. **Attestation bundling**: Should gittuf metadata be bundled with archives for air-gapped verification?
3. **Incremental adoption**: How to handle repos transitioning to gittuf (partial coverage)?
4. **Key distribution**: Best practices for distributing/rotating root keys to consumers?

## References

- [gittuf website](https://gittuf.dev/)
- [gittuf GitHub repository](https://github.com/gittuf/gittuf)
- [gittuf design document](https://github.com/gittuf/gittuf/blob/main/docs/design-document.md)
- [LWN: Securing Git repositories with gittuf](https://lwn.net/Articles/972467/)
- [TUF specification](https://theupdateframework.io/)
- [SLSA specification](https://slsa.dev/)

## Conclusion

Integrating gittuf with Blob is **feasible and strategically valuable**. It would complete Blob's provenance story by adding source authorization verification alongside existing build provenance. The main technical challenge is gittuf's internal-only Go packages, which should be addressed through upstream collaboration.

Recommended next steps:
1. Open discussion with gittuf maintainers about SDK export
2. Prototype basic integration using CLI wrapper
3. Develop comprehensive test suite with gittuf-enabled repositories
4. Iterate toward production-quality implementation
