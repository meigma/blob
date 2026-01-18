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
2. **Sign**: Uses Sigstore cosign with GitHub OIDC (keyless)
3. **Attest**: Attaches SLSA provenance via `actions/attest-build-provenance`
4. **Verify**: Pulls with full policy enforcement

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
# Pull with signature verification
go run . pull \
  --ref ghcr.io/meigma/blob/provenance-example:latest \
  --policy ./policy/policy.rego

# Specify expected signer identity
go run . pull \
  --ref ghcr.io/meigma/blob/provenance-example:latest \
  --issuer https://token.actions.githubusercontent.com \
  --subject https://github.com/meigma/blob/.github/workflows/provenance.yml@refs/heads/main
```

## Policy Customization

The OPA policy in `policy/policy.rego` validates SLSA provenance. Customize it for your needs:

### Allow Specific Organizations

```rego
# Only allow builds from your organization
allowed_orgs := {
    "your-org",
}
```

### Add Custom Builders

```rego
# Trust additional builders
trusted_builders := {
    "https://github.com/actions/runner/github-hosted",
    "https://your-custom-builder.example.com",
}
```

### Require Specific Workflows

```rego
# Only allow builds from specific workflows
allow if {
    some att in input.attestations
    is_slsa_provenance(att)
    att.predicate.buildDefinition.externalParameters.workflow.path == ".github/workflows/release.yml"
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         GitHub Actions                               │
├─────────────────────────────────────────────────────────────────────┤
│  1. Build Archive      2. Sign (Cosign)     3. Attest (SLSA)        │
│  ┌──────────────┐      ┌──────────────┐     ┌──────────────┐        │
│  │ blob.Create  │─────▶│ cosign sign  │────▶│ attest-build │        │
│  │ client.Push  │      │ (keyless)    │     │ -provenance  │        │
│  └──────────────┘      └──────────────┘     └──────────────┘        │
│         │                     │                    │                 │
│         ▼                     ▼                    ▼                 │
│  ┌────────────────────────────────────────────────────────────┐     │
│  │                    OCI Registry (GHCR)                      │     │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │     │
│  │  │  Manifest   │  │  Sigstore   │  │ SLSA Attestation    │ │     │
│  │  │  (archive)  │◀─│  Bundle     │  │ (in-toto)           │ │     │
│  │  └─────────────┘  └─────────────┘  └─────────────────────┘ │     │
│  │        ▲              referrer         referrer             │     │
│  └────────┼────────────────────────────────────────────────────┘     │
└───────────┼─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                           Consumer                                   │
├─────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐      ┌──────────────┐     ┌──────────────┐        │
│  │ client.Pull  │─────▶│ sigstore     │────▶│ opa.Policy   │        │
│  │              │      │ .Policy      │     │ (SLSA check) │        │
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
| `policy/policy.rego` | OPA policy for SLSA validation |

## Dependencies

- `github.com/meigma/blob` - Archive creation
- `github.com/meigma/blob/client` - OCI registry operations
- `github.com/meigma/blob/policy/sigstore` - Signature verification
- `github.com/meigma/blob/policy/opa` - Attestation policy engine

External tools (CI only):
- [cosign](https://github.com/sigstore/cosign) - Keyless signing
- [actions/attest-build-provenance](https://github.com/actions/attest-build-provenance) - SLSA attestation
