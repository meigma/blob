# Proposal: Gittuf Integration for Source Provenance

**Status:** Proposal
**Author:** Claude
**Date:** 2026-01-20

## Executive Summary

This proposal explores integrating [gittuf](https://gittuf.dev/) with Blob to provide **source provenance** verification. While Blob currently offers robust build provenance through SLSA attestations (proving *how* an archive was built), it lacks verification of *what* authorized the source code changes. Gittuf fills this gap by providing cryptographic proof that Git repository changes followed security policies.

**Recommendation:** Integration is **feasible and straightforward**. Gittuf's `experimental/gittuf` package provides a complete public Go API (`Clone()`, `VerifyRef()`, `LoadPublicKey()`) that can be directly imported - no CLI wrapping or upstream negotiation required.

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

The gittuf project provides a **public Go API** in the `experimental/gittuf` package:

```
github.com/gittuf/gittuf/
├── experimental/
│   └── gittuf/           # Public API (importable!)
│       ├── repository.go # Repository type and methods
│       ├── verify.go     # VerifyRef, VerifyRefFromEntry
│       ├── keys.go       # LoadPublicKey, LoadSigner
│       ├── policy.go     # Policy management
│       ├── rsl.go        # RSL operations
│       └── options/      # Verification options
└── internal/             # Internal implementation details
```

**Key finding:** The `experimental/gittuf` package exports all the types and functions needed for integration. No upstream negotiation, CLI wrapping, or vendoring required.

### Available gittuf API

The following functions are directly importable from `github.com/gittuf/gittuf/experimental/gittuf`:

```go
// Repository loading
func LoadRepository(repositoryPath string) (*Repository, error)
func Clone(ctx context.Context, remoteURL, dir, initialBranch string,
    expectedRootKeys []tuf.Principal, bare bool) (*Repository, error)

// Key management
func LoadPublicKey(keyRef string) (tuf.Principal, error)

// Core verification (what we need!)
func (r *Repository) VerifyRef(ctx context.Context, refName string,
    opts ...verifyopts.Option) error
func (r *Repository) VerifyRefFromEntry(ctx context.Context, refName, entryID string,
    opts ...verifyopts.Option) error
func (r *Repository) VerifyMergeable(ctx context.Context, targetRef, featureRef string,
    opts ...verifymergeableopts.Option) (bool, error)

// Policy inspection
func (r *Repository) HasPolicy() (bool, error)
func (r *Repository) ListRules(ctx context.Context, targetRef string) ([]*DelegationWithDepth, error)
```

This makes integration **significantly more straightforward** than initially assessed.

### Proposed Blob Policy Implementation

Using the gittuf API directly, the implementation becomes straightforward:

```go
// policy/gittuf/policy.go
package gittuf

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    gittuflib "github.com/gittuf/gittuf/experimental/gittuf"
    "github.com/gittuf/gittuf/internal/tuf"
    "github.com/meigma/blob/registry"
)

// Policy implements registry.Policy for gittuf verification.
type Policy struct {
    repoURL      string
    rootKeys     []tuf.Principal
    refName      string           // e.g., "refs/heads/main"
    cloneDir     string           // temp directory for clone
    verifyOpts   []verifyopts.Option
}

// Evaluate implements registry.Policy.
func (p *Policy) Evaluate(ctx context.Context, req registry.PolicyRequest) error {
    // 1. Extract source commit from SLSA provenance
    commit, refName, err := p.extractSourceInfo(ctx, req)
    if err != nil {
        return fmt.Errorf("gittuf: %w", err)
    }

    // 2. Clone repository with gittuf verification
    //    Clone() validates root keys automatically
    tmpDir, err := os.MkdirTemp("", "gittuf-verify-*")
    if err != nil {
        return fmt.Errorf("gittuf: failed to create temp dir: %w", err)
    }
    defer os.RemoveAll(tmpDir)

    repo, err := gittuflib.Clone(ctx, p.repoURL, tmpDir, "", p.rootKeys, true)
    if err != nil {
        return fmt.Errorf("gittuf: clone failed: %w", err)
    }

    // 3. Verify the reference using gittuf's native verification
    if err := repo.VerifyRef(ctx, refName, p.verifyOpts...); err != nil {
        return fmt.Errorf("gittuf: verification failed for %s: %w", refName, err)
    }

    return nil
}

// NewPolicy creates a gittuf source provenance policy.
func NewPolicy(opts ...PolicyOption) (*Policy, error)

// PolicyOption configures the policy.
type PolicyOption func(*Policy) error

// WithRepository sets the source repository URL.
func WithRepository(url string) PolicyOption

// WithRootKeys sets the trusted root key fingerprints.
// Keys are loaded using gittuf's LoadPublicKey() which supports:
// - SSH keys: "ssh:SHA256:..." or path to .pub file
// - GPG keys: "gpg:FINGERPRINT"
// - Sigstore/Fulcio: "fulcio:email@example.com"
func WithRootKeys(keyRefs ...string) PolicyOption

// WithRefName specifies the Git ref to verify (default: from SLSA provenance).
func WithRefName(ref string) PolicyOption

// --- Convenience constructors ---

// GitHubRepository creates a policy for a GitHub-hosted repository.
func GitHubRepository(owner, repo string, opts ...PolicyOption) (*Policy, error) {
    url := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
    return NewPolicy(append([]PolicyOption{WithRepository(url)}, opts...)...)
}
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

- [ ] Create `policy/gittuf` Go module with dependency on `github.com/gittuf/gittuf`
- [ ] Implement `registry.Policy` using `gittuf.Clone()` + `repo.VerifyRef()`
- [ ] Add source commit/ref extraction from SLSA provenance attestations
- [ ] Support `WithRootKeys()` using `gittuf.LoadPublicKey()` (SSH, GPG, Fulcio)
- [ ] Add comprehensive tests with gittuf-enabled test repositories

### Phase 2: Production Hardening

- [ ] Add caching for cloned repositories (with TTL-based invalidation)
- [ ] Implement shallow/sparse clone for faster verification
- [ ] Add structured logging and error messages
- [ ] Performance benchmarks and optimization
- [ ] Handle network failures with retry logic

### Phase 3: Advanced Features

- [ ] Support `repo.VerifyRefFromEntry()` for pinning to specific RSL entry
- [ ] `repo.VerifyMergeable()` for PR-style workflows
- [ ] Path-specific policies using gittuf's delegation rules
- [ ] Integration with gittuf GitHub App attestations

### Phase 4: Ecosystem

- [ ] GitHub Action for creating gittuf-aware Blob archives
- [ ] Documentation and tutorials
- [ ] Example workflows for common scenarios
- [ ] Explore bundled gittuf metadata for air-gapped verification

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

1. **~~SDK availability~~**: ✅ Resolved - `experimental/gittuf` package provides full public API
2. **Attestation bundling**: Should gittuf metadata be bundled with archives for air-gapped verification?
3. **Incremental adoption**: How to handle repos transitioning to gittuf (partial coverage)?
4. **Key distribution**: Best practices for distributing/rotating root keys to consumers?
5. **Experimental stability**: The package is in `experimental/` - what's the stability guarantee?
6. **Performance**: Clone-based verification may be slow; explore shallow/sparse clone options

## References

- [gittuf website](https://gittuf.dev/)
- [gittuf GitHub repository](https://github.com/gittuf/gittuf)
- [gittuf experimental/gittuf package](https://github.com/gittuf/gittuf/tree/main/experimental/gittuf)
- [gittuf Go documentation](https://pkg.go.dev/github.com/gittuf/gittuf/experimental/gittuf)
- [gittuf design document](https://github.com/gittuf/gittuf/blob/main/docs/design-document.md)
- [LWN: Securing Git repositories with gittuf](https://lwn.net/Articles/972467/)
- [TUF specification](https://theupdateframework.io/)
- [SLSA specification](https://slsa.dev/)

## Conclusion

Integrating gittuf with Blob is **feasible and straightforward**. The `experimental/gittuf` package provides a complete public Go API including `Clone()`, `LoadRepository()`, `VerifyRef()`, and key management functions - everything needed for integration without upstream negotiation or CLI wrapping.

This would complete Blob's provenance story:
- **Sigstore**: WHO signed the archive
- **SLSA**: HOW the archive was built
- **Gittuf**: WHETHER source changes were authorized

Recommended next steps:
1. Create `policy/gittuf` Go module with gittuf dependency
2. Implement `registry.Policy` using `gittuf.Clone()` and `repo.VerifyRef()`
3. Add source commit extraction from SLSA provenance
4. Develop test suite with gittuf-enabled test repositories
5. Optimize for performance (caching, shallow clones)
6. Document root key distribution best practices
