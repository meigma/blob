---
sidebar_position: 8
---

# Provenance & Signing

Verify archive integrity and provenance using Sigstore signatures and SLSA attestations.

## Overview

Blob integrates with the OCI ecosystem's supply chain security tools to provide cryptographic verification of archives:

- **Sigstore signing**: Keyless signatures using GitHub Actions OIDC tokens
- **SLSA provenance**: Build attestations describing how archives were created
- **Policy helpers**: Simple APIs for common verification patterns
- **OPA policies**: Flexible Rego-based policy evaluation for advanced use cases

These capabilities ensure that archives come from trusted sources and were built through authorized processes.

## The Verification Chain

```
Signature (Sigstore)
    │
    ▼ verifies
Manifest (OCI)
    │
    ▼ contains digest of
Index + Data Blobs
    │
    ▼ contains
Per-file SHA256 Hashes
```

Sigstore signatures bind the manifest to a verified identity. The manifest contains digests for the index and data blobs. The index contains per-file hashes. This chain ensures that any tampering—at any level—is detectable.

## Signing Archives

The blob package provides built-in signing via the `Client.Sign()` method. This creates Sigstore signatures and attaches them as OCI 1.1 referrer artifacts.

### Keyless Signing (Recommended for CI)

For GitHub Actions workflows, use keyless signing with ambient OIDC credentials:

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy/sigstore"
)

// Create signer with keyless configuration
signer, err := sigstore.NewSigner(
    sigstore.WithEphemeralKey(),                    // Generate ephemeral keypair
    sigstore.WithFulcio("https://fulcio.sigstore.dev"),  // Get certificate from Fulcio
    sigstore.WithRekor("https://rekor.sigstore.dev"),    // Record in transparency log
    sigstore.WithAmbientCredentials(),              // Auto-detect OIDC from CI
)
if err != nil {
    return err
}

client, err := blob.NewClient(blob.WithDockerConfig())
if err != nil {
    return err
}

// Push the archive
err = client.Push(ctx, "ghcr.io/myorg/archive:v1", "./assets")
if err != nil {
    return err
}

// Sign the manifest (creates OCI 1.1 referrer)
sigDigest, err := client.Sign(ctx, "ghcr.io/myorg/archive:v1", signer)
if err != nil {
    return err
}
fmt.Printf("Signed! Signature digest: %s\n", sigDigest)
```

The signature is attached to the manifest as an OCI referrer artifact, which can be discovered and verified by consumers.

### Signer Options

| Option | Description |
|--------|-------------|
| `WithEphemeralKey()` | Generate ephemeral keypair (recommended for keyless signing) |
| `WithFulcio(url)` | Enable Fulcio certificate issuance |
| `WithRekor(url)` | Enable Rekor transparency log |
| `WithAmbientCredentials()` | Auto-detect OIDC token from CI environment |
| `WithIDToken(token)` | Use static OIDC token |
| `WithPrivateKey(key)` | Use existing `crypto.Signer` |
| `WithPrivateKeyPEM(data, password)` | Use PEM-encoded private key |

### Key-Based Signing

For environments without OIDC, use a private key:

```go
import (
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
)

// Generate or load a private key
key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

signer, err := sigstore.NewSigner(
    sigstore.WithPrivateKey(key),
    sigstore.WithRekor("https://rekor.sigstore.dev"),
)
```

Or load from a PEM file:

```go
pemData, _ := os.ReadFile("private-key.pem")
signer, err := sigstore.NewSigner(
    sigstore.WithPrivateKeyPEM(pemData, nil), // nil password for unencrypted
    sigstore.WithRekor("https://rekor.sigstore.dev"),
)
```

### Complete Push and Sign Workflow

```go
func pushAndSign(ctx context.Context, ref, srcDir string) error {
    // Create client
    client, err := blob.NewClient(blob.WithDockerConfig())
    if err != nil {
        return fmt.Errorf("create client: %w", err)
    }

    // Push archive with compression
    err = client.Push(ctx, ref, srcDir, blob.PushWithCompression(blob.CompressionZstd))
    if err != nil {
        return fmt.Errorf("push: %w", err)
    }

    // Create keyless signer (CI environment)
    signer, err := sigstore.NewSigner(
        sigstore.WithEphemeralKey(),
        sigstore.WithFulcio("https://fulcio.sigstore.dev"),
        sigstore.WithRekor("https://rekor.sigstore.dev"),
        sigstore.WithAmbientCredentials(),
    )
    if err != nil {
        return fmt.Errorf("create signer: %w", err)
    }

    // Sign the manifest
    sigDigest, err := client.Sign(ctx, ref, signer)
    if err != nil {
        return fmt.Errorf("sign: %w", err)
    }

    fmt.Printf("Pushed and signed: %s\n", ref)
    fmt.Printf("Signature digest: %s\n", sigDigest)
    return nil
}
```

## Quick Start

For archives built and signed in GitHub Actions, use the high-level helpers:

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy"
    "github.com/meigma/blob/policy/sigstore"
    "github.com/meigma/blob/policy/slsa"
)

// Verify both signature and provenance
sigPolicy, err := sigstore.GitHubActionsPolicy("myorg/myrepo")
if err != nil {
    return err
}
slsaPolicy, err := slsa.GitHubActionsWorkflow("myorg/myrepo")
if err != nil {
    return err
}

c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy)),
)
if err != nil {
    return err
}

// Pull fails if verification fails
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
```

This covers the most common case: verifying that archives come from your GitHub Actions workflows.

## Sigstore Signature Verification

The `policy/sigstore` package verifies Sigstore signatures attached to OCI manifests.

### GitHub Actions Signatures

For workflows using keyless signing with GitHub Actions OIDC:

```go
import "github.com/meigma/blob/policy/sigstore"

// Accept any workflow from the repo
sigPolicy, err := sigstore.GitHubActionsPolicy("myorg/myrepo")

// Restrict to specific branches
sigPolicy, err := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main", "release/*"),
)

// Restrict to release tags only
sigPolicy, err := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowTags("v*"),
)

// Combine branch and tag restrictions
sigPolicy, err := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main"),
    sigstore.AllowTags("v*"),
)
```

The `AllowBranches` and `AllowTags` options accept simple wildcards (`*` matches any characters).

### Advanced: Custom Identity Verification

For non-GitHub-Actions signers or custom OIDC providers, use `NewPolicy` with `WithIdentity`:

```go
sigPolicy, err := sigstore.NewPolicy(
    sigstore.WithIdentity(
        "https://accounts.google.com",           // OIDC issuer
        "ci-bot@mycompany.iam.gserviceaccount.com",  // Subject
    ),
)
```

For GitHub Actions, the issuer is `https://token.actions.githubusercontent.com` and the subject follows the pattern `https://github.com/OWNER/REPO/.github/workflows/WORKFLOW@REF`.

## SLSA Provenance Verification

The `policy/slsa` package validates SLSA provenance attestations attached to OCI manifests.

### GitHub Actions Workflows

Validate that archives were built by specific GitHub Actions workflows:

```go
import "github.com/meigma/blob/policy/slsa"

// Accept any workflow from the repo
slsaPolicy, err := slsa.GitHubActionsWorkflow("myorg/myrepo")

// Require a specific workflow file
slsaPolicy, err := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowPath(".github/workflows/release.yml"),
)

// Restrict to specific branches
slsaPolicy, err := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowBranches("main"),
)

// Restrict to release tags
slsaPolicy, err := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowTags("v*"),
)

// Full example with all restrictions
slsaPolicy, err := slsa.GitHubActionsWorkflow("myorg/myrepo",
    slsa.WithWorkflowPath(".github/workflows/release.yml"),
    slsa.WithWorkflowBranches("main"),
    slsa.WithWorkflowTags("v*"),
)
```

### Builder and Source Validation

For more granular control over provenance requirements:

```go
// Require a specific SLSA builder
builderPolicy := slsa.RequireBuilder(
    "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v2.0.0",
)

// Require builds from a specific source repository
sourcePolicy := slsa.RequireSource("https://github.com/myorg/myrepo",
    slsa.WithBranches("main", "release/*"),
    slsa.WithTags("v*"),
)
```

## Composing Policies

The `policy` package provides utilities for combining multiple policies.

### Require All Policies Pass (AND)

```go
import "github.com/meigma/blob/policy"

// Both signature AND provenance must be valid
combined := policy.RequireAll(sigPolicy, slsaPolicy)

c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(combined),
)
```

Policies are evaluated in order. Evaluation stops at the first failure.

### Accept Any Matching Policy (OR)

```go
// Accept archives from either repository
multiSource := policy.RequireAny(
    slsa.GitHubActionsWorkflow("myorg/repo1"),
    slsa.GitHubActionsWorkflow("myorg/repo2"),
)
```

### Nested Composition

```go
// Require signature, AND accept provenance from any of multiple repos
combined := policy.RequireAll(
    sigPolicy,
    policy.RequireAny(
        slsa.GitHubActionsWorkflow("myorg/repo1"),
        slsa.GitHubActionsWorkflow("myorg/repo2"),
    ),
)
```

## Advanced: Custom Policies with OPA

For validation logic beyond what the built-in helpers provide, use OPA with custom Rego policies. This is useful when you need:

- Complex conditional logic
- Custom attestation formats
- Organization-specific policy rules
- Multi-tenant verification requirements

### Basic OPA Policy

```go
import "github.com/meigma/blob/policy/opa"

// Create an OPA policy from a Rego file
opaPolicy, err := opa.NewPolicy(
    opa.WithPolicyFile("./policy.rego"),
)
if err != nil {
    return err
}

c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(opaPolicy),
)
```

### Writing Rego Policies

OPA policies are written in Rego. The policy engine provides attestations as input:

```rego
package blob.policy

import rego.v1

# Default deny
default allow := false

# Allow if we have valid SLSA provenance from GitHub Actions
allow if {
    some att in input.attestations
    att.predicateType == "https://slsa.dev/provenance/v1"
    is_github_actions_builder(att)
    is_allowed_repository(att)
}

# Verify the builder is GitHub Actions
is_github_actions_builder(att) if {
    builder_id := att.predicate.runDetails.builder.id
    startswith(builder_id, "https://github.com/")
    contains(builder_id, "/.github/workflows/")
}

# Allow only specific organizations
allowed_orgs := {"myorg", "trustedorg"}

is_allowed_repository(att) if {
    repo := att.predicate.buildDefinition.externalParameters.workflow.repository
    some org in allowed_orgs
    startswith(repo, concat("", ["https://github.com/", org, "/"]))
}

# Provide error messages for denials
deny contains msg if {
    count(input.attestations) == 0
    msg := "no attestations found"
}
```

### Policy Input Structure

The OPA policy receives this input structure:

```json
{
  "manifest": {
    "reference": "ghcr.io/myorg/myarchive:v1",
    "digest": "sha256:abc123...",
    "mediaType": "application/vnd.oci.image.manifest.v1+json"
  },
  "attestations": [
    {
      "_type": "https://in-toto.io/Statement/v1",
      "predicateType": "https://slsa.dev/provenance/v1",
      "subject": [...],
      "predicate": {
        "buildDefinition": {
          "buildType": "https://actions.github.io/buildtypes/workflow/v1",
          "externalParameters": {
            "workflow": {
              "repository": "https://github.com/myorg/myrepo",
              "path": ".github/workflows/release.yml"
            }
          }
        },
        "runDetails": {
          "builder": {
            "id": "https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/main"
          }
        }
      }
    }
  ]
}
```

### Common Rego Patterns

**Require specific workflows:**

```rego
allow if {
    some att in input.attestations
    att.predicate.buildDefinition.externalParameters.workflow.path == ".github/workflows/release.yml"
}
```

**Require builds from specific branches:**

```rego
allow if {
    some att in input.attestations
    builder_id := att.predicate.runDetails.builder.id
    contains(builder_id, "@refs/heads/main")
}
```

**Deny builds from forks:**

```rego
deny contains msg if {
    some att in input.attestations
    repo := att.predicate.buildDefinition.externalParameters.workflow.repository
    not startswith(repo, "https://github.com/myorg/")
    msg := sprintf("build from untrusted repository: %s", [repo])
}
```

## CI/CD Integration

### GitHub Actions Workflow

A complete workflow for building and signing archives using the blob library's built-in signing:

```yaml
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

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build, push, and sign archive
        run: |
          # Build your CLI tool that uses client.Push() and client.Sign()
          go build -o release-tool ./cmd/release

          # Push and sign with --sign flag (using Client.Sign() internally)
          ./release-tool push \
            --ref ghcr.io/${{ github.repository }}/archive:${{ github.ref_name }} \
            --sign
```

The `--sign` flag triggers `Client.Sign()` with keyless signing. In your CLI tool:

```go
// In your push command implementation
if cfg.sign {
    signer, err := sigstore.NewSigner(
        sigstore.WithEphemeralKey(),
        sigstore.WithFulcio("https://fulcio.sigstore.dev"),
        sigstore.WithRekor("https://rekor.sigstore.dev"),
        sigstore.WithAmbientCredentials(), // Detects GitHub Actions OIDC
    )
    if err != nil {
        return err
    }
    _, err = client.Sign(ctx, ref, signer)
    if err != nil {
        return err
    }
}
```

### Using External Tools (Alternative)

You can also use external signing tools like Cosign or GitHub's attestation action:

```yaml
      - name: Build and push archive
        id: push
        run: |
          go run ./cmd/push --ref ghcr.io/${{ github.repository }}/archive:${{ github.ref_name }}
          echo "digest=sha256:..." >> "$GITHUB_OUTPUT"

      - name: Sign with Cosign
        uses: sigstore/cosign-installer@v3
      - run: cosign sign --yes ghcr.io/${{ github.repository }}/archive@${{ steps.push.outputs.digest }}

      - name: Attest provenance
        uses: actions/attest-build-provenance@v2
        with:
          subject-name: ghcr.io/${{ github.repository }}/archive
          subject-digest: ${{ steps.push.outputs.digest }}
          push-to-registry: true
```

### Required Permissions

| Permission | Purpose | Required For |
|------------|---------|--------------|
| `id-token: write` | OIDC token for keyless Sigstore signing | `Client.Sign()` with `WithAmbientCredentials()` |
| `packages: write` | Push to GitHub Container Registry | `Client.Push()` and `Client.Sign()` |
| `attestations: write` | Attach SLSA attestations | `actions/attest-build-provenance` (optional) |

## Complete Example

The repository includes a complete provenance example at [`examples/provenance/`](https://github.com/meigma/blob/tree/main/examples/provenance):

```go
// From examples/provenance/pull.go
sigPolicy, err := sigstore.GitHubActionsPolicy(repo)
if err != nil {
    return err
}
slsaPolicy, err := slsa.GitHubActionsWorkflow(repo)
if err != nil {
    return err
}

c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy)),
)
if err != nil {
    return err
}

archive, err := c.Pull(ctx, ref)
```

Run the example:

```bash
git clone https://github.com/meigma/blob
cd blob/examples/provenance
go build -o provenance .

# Push an archive (for local testing)
./provenance push --ref ttl.sh/my-test-$(date +%s):1h

# Pull with verification (requires signed archive)
./provenance pull --ref ghcr.io/meigma/blob/provenance-example:latest
```

## Skipping Verification

For local development or testing, verification can be skipped:

```go
// No policies = no verification
c, _ := blob.NewClient(blob.WithDockerConfig())
archive, err := c.Pull(ctx, ref)
```

This is appropriate for local testing but should never be used in production.

## Troubleshooting

### Signing Errors

#### "no keypair configured"

When creating a signer, you must configure a keypair using `WithEphemeralKey()` or `WithPrivateKey()`:

```go
signer, err := sigstore.NewSigner(
    sigstore.WithEphemeralKey(),  // Required
    // ... other options
)
```

#### "sigstore get token" errors

The signer couldn't obtain an OIDC token. Ensure:
- You're running in a CI environment with OIDC support (GitHub Actions)
- The `id-token: write` permission is set in your workflow
- `WithAmbientCredentials()` or `WithIDToken()` is configured

#### "push signature blob" or "push referrer manifest" errors

Registry access issues. Check:
- You're authenticated to the registry (`WithDockerConfig()`)
- The registry supports OCI 1.1 referrers
- You have write access to the repository

### Verification Errors

#### "no signature found"

The archive has no Sigstore signature attached. Ensure your CI pipeline includes `Client.Sign()` or the cosign signing step.

#### "signature verification failed"

The signature exists but verification failed. Check:
- The repository in `GitHubActionsPolicy` matches your signing workflow
- The branch/tag restrictions match where the workflow ran
- The Sigstore transparency log is accessible

#### "policy evaluation failed: allow = false"

The OPA policy denied the attestation. Check:
- Attestations are attached to the manifest
- The attestation predicate type matches your policy
- The builder/repository constraints in your policy match the attestation

#### "no attestations found"

No SLSA attestations are attached. Ensure your CI pipeline includes the `actions/attest-build-provenance` step with `push-to-registry: true`.
