// Package sigstore provides a client.Policy implementation for verifying
// blob archives using Sigstore signatures.
//
// This package implements [github.com/meigma/blob/registry.Policy] using the
// sigstore-go library for cryptographic verification of OCI artifact signatures.
//
// # Separate Module
//
// This package is a separate Go module (github.com/meigma/blob/policy/sigstore)
// to isolate the sigstore-go dependency. This design allows users who don't need
// signature verification to import github.com/meigma/blob without pulling in
// sigstore-go and its transitive dependencies (protobuf, gRPC, OIDC, etc.).
//
// # Verification Policy
//
// The Policy verifies that pulled archives have valid Sigstore signatures
// attached as OCI referrers. It fetches signature bundles from the registry
// and validates them against a trusted root.
//
// Example:
//
//	policy, err := sigstore.NewPolicy(
//	    sigstore.WithIdentity("https://accounts.google.com", "user@example.com"),
//	)
//	if err != nil {
//	    return err
//	}
//
//	c := client.New(
//	    client.WithDockerConfig(),
//	    client.WithPolicy(policy),
//	)
//
//	// Pull will fail if the archive is not signed by the expected identity
//	archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
//
// # Signing
//
// The Signer creates Sigstore bundles that can be stored as OCI referrer artifacts.
// Multiple signing modes are supported:
//
//   - Ephemeral keys with Fulcio CA (keyless signing via OIDC)
//   - Local key files (private keys in PEM format)
//
// Example:
//
//	signer, err := sigstore.NewSigner(
//	    sigstore.WithEphemeralKey(),
//	    sigstore.WithFulcio("https://fulcio.sigstore.dev"),
//	    sigstore.WithRekor("https://rekor.sigstore.dev"),
//	)
package sigstore
