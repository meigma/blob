# Security Policy

## Reporting Security Issues

If you discover a security vulnerability in Blob, please report it through GitHub's private vulnerability reporting feature:

1. Go to the [Security tab](../../security) of this repository
2. Click "Report a vulnerability"
3. Provide a detailed description of the issue

**Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Include as much of the following information as possible to help us understand and resolve the issue:

- Type of issue (e.g., path traversal, arbitrary file write, credential exposure)
- Full paths of source file(s) related to the issue
- Location of the affected source code (tag/branch/commit or direct URL)
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue and how an attacker might exploit it

## Supported Versions

We provide security updates for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.x     | :white_check_mark: |

## Response Timeline

- **Initial Response**: We aim to acknowledge receipt of your vulnerability report within 3 business days.
- **Status Update**: We will provide a more detailed response within 10 business days, including our assessment and expected timeline for a fix.
- **Resolution**: We strive to resolve critical vulnerabilities within 30 days of the initial report.

## Disclosure Policy

We follow a coordinated disclosure process:

1. Security issues are handled privately until a fix is available.
2. Once a fix is ready, we will create a security advisory and release a patched version.
3. We will publicly disclose the vulnerability after users have had reasonable time to update.
4. Credit will be given to the reporter (unless anonymity is preferred) in the security advisory.

## Security Practices

Blob implements the following security measures:

### Artifact Signing and Verification

Blob supports [Sigstore](https://sigstore.dev) signing and verification for archives:

- **Keyless signing**: Sign archives using OIDC identity (GitHub Actions, Google, Microsoft) without managing private keys
- **Key-based signing**: Sign with your own private keys (ECDSA, RSA, Ed25519)
- **Policy-based verification**: Require signatures from specific OIDC issuers and subjects before pulling
- **SLSA provenance**: Verify build provenance from GitHub Actions workflows

```go
import (
    "github.com/meigma/blob"
    "github.com/meigma/blob/policy"
    "github.com/meigma/blob/policy/sigstore"
    "github.com/meigma/blob/policy/slsa"
)

// Require signatures from GitHub Actions
sigPolicy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo",
    sigstore.AllowBranches("main"),
)

// Require SLSA provenance
slsaPolicy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo")

// Pull fails if verification fails
c, _ := blob.NewClient(
    blob.WithDockerConfig(),
    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy)),
)
archive, err := c.Pull(ctx, "ghcr.io/myorg/config:v1")
```

See [Provenance & Signing](https://blob.meigma.dev/docs/guides/provenance) for complete documentation.

### Per-File Integrity

- Every file in the archive has a SHA256 hash stored in the index
- Hashes are verified automatically when reading file content
- Tamper with a single byte and verification fails instantly

### Path Traversal Protection

- Archives are validated during extraction to prevent path traversal attacks
- Paths are "jailed" to the destination directory

### Code Quality

- Static analysis with [gosec](https://github.com/securego/gosec) security scanner
- Comprehensive linting with golangci-lint
- Race detection enabled in all tests

## Third-Party Dependencies

For vulnerabilities in third-party dependencies used by Blob:

- If the vulnerability affects Blob, please report it through our security reporting process above
- For vulnerabilities in upstream projects, please report directly to those projects:
  - **Go dependencies**: Use the project's security reporting mechanism or [Go vulnerability database](https://pkg.go.dev/vuln/)
  - **OCI/Container issues**: Report to the respective CNCF project

## Security-Related Configuration

When using Blob:

- Registry credentials are read from Docker's credential store (`~/.docker/config.json`)
- Use credential helpers for enhanced security rather than storing plain credentials
- Cached data is stored locally; ensure appropriate file permissions on cache directories
- When pushing to registries, use authenticated connections and verify TLS certificates
- **Enable policy verification** in production by configuring `WithPolicy()`
- Private signing keys should be stored securely with appropriate file permissions

## Learning More

- [Blob Documentation](https://blob.meigma.dev)
- [Provenance & Signing Guide](https://blob.meigma.dev/docs/guides/provenance)
- [Integrity Verification](https://blob.meigma.dev/docs/explanation/integrity)
- [Sigstore Documentation](https://docs.sigstore.dev/)
