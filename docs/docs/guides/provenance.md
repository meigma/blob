---
sidebar_position: 8
---

# Provenance & Signing

Verify archive integrity and provenance using Sigstore signatures and SLSA attestations.

## Overview

Blob integrates with the OCI ecosystem's supply chain security tools to provide cryptographic verification of archives:

- **Sigstore signing**: Keyless signatures using GitHub Actions OIDC tokens
- **SLSA provenance**: Build attestations describing how archives were created
- **OPA policies**: Flexible policy evaluation for attestation validation

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

## Signing with Sigstore

The `policy/sigstore` package verifies Sigstore signatures attached to OCI manifests as referrers.

### Basic Signature Verification

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy/sigstore"
)

// Create a signature verification policy
sigPolicy, err := sigstore.NewPolicy()
if err != nil {
    return err
}

// Create client with policy
c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(sigPolicy),
)
if err != nil {
    return err
}

// Pull verifies the signature automatically
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
if err != nil {
    // Fails if signature is missing or invalid
    return err
}
```

### Requiring Specific Identities

For production, require signatures from specific issuers and subjects:

```go
sigPolicy, err := sigstore.NewPolicy(
    sigstore.WithIdentity(
        "https://token.actions.githubusercontent.com",  // GitHub Actions OIDC
        "https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/main",
    ),
)
```

This ensures archives are signed by a specific GitHub Actions workflow, not just any valid Sigstore signature.

## SLSA Attestations with OPA

The `policy/opa` package validates SLSA provenance attestations using Open Policy Agent.

### Basic Attestation Verification

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy/opa"
)

// Create an OPA policy from a Rego file
opaPolicy, err := opa.NewPolicy(
    opa.WithPolicyFile("./policy.rego"),
)
if err != nil {
    return err
}

// Create client with policy
c, err := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(opaPolicy),
)
if err != nil {
    return err
}

// Pull evaluates the policy against attestations
archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
```

### Combining Policies

Use both signature and attestation verification together:

```go
c, _ := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(sigPolicy),   // Verify signature first
    blob.WithPolicy(opaPolicy),   // Then check attestations
)
```

Policies are evaluated in order. If any policy fails, the pull is rejected.

## Writing OPA Policies

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

### Common Policy Patterns

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

A complete workflow for building, signing, and attesting archives:

```yaml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: read
  packages: write
  id-token: write      # Required for keyless signing
  attestations: write  # Required for attestations

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build and push archive
        id: push
        run: |
          # Build your archive and push to registry
          go run ./cmd/push --ref ghcr.io/${{ github.repository }}/archive:${{ github.ref_name }}
          # Output the digest for attestation
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

| Permission | Purpose |
|------------|---------|
| `id-token: write` | OIDC token for keyless Sigstore signing |
| `attestations: write` | Attach SLSA attestations to artifacts |
| `packages: write` | Push to GitHub Container Registry |

## Complete Example

The repository includes a complete provenance example at [`examples/provenance/`](https://github.com/meigma/blob/tree/main/examples/provenance):

```bash
# Clone and build
git clone https://github.com/meigma/blob
cd blob/examples/provenance
go build -o provenance .

# Push an archive (for local testing)
./provenance push --ref ttl.sh/my-test-$(date +%s):1h

# Pull with verification (requires signed archive)
./provenance pull \
  --ref ghcr.io/meigma/blob/provenance-example:latest \
  --policy ./policy/policy.rego
```

The example includes:

- **`push.go`**: Archive creation and registry push
- **`pull.go`**: Pull with Sigstore and OPA policy verification
- **`policy/policy.rego`**: Sample SLSA validation policy
- **`.github/workflows/provenance.yml`**: Complete CI/CD workflow

## Skipping Verification

For local development or testing, verification can be skipped:

```go
// No policies = no verification
c, _ := blob.NewClient(blob.WithDockerConfig())
archive, err := c.Pull(ctx, ref)
```

This is appropriate for local testing but should never be used in production.

## Troubleshooting

### "no signature found"

The archive has no Sigstore signature attached. Ensure your CI pipeline includes the cosign signing step.

### "signature verification failed"

The signature exists but verification failed. Check:
- The signing identity matches your `WithIdentity()` configuration
- The signature hasn't expired
- The Sigstore transparency log is accessible

### "policy evaluation failed: allow = false"

The OPA policy denied the attestation. Check:
- Attestations are attached to the manifest
- The attestation predicate type matches your policy
- The builder/repository constraints in your policy match the attestation

### "no attestations found"

No SLSA attestations are attached. Ensure your CI pipeline includes the `actions/attest-build-provenance` step with `push-to-registry: true`.
