// Package slsa provides Go-native policies for validating SLSA provenance
// attestations without requiring OPA or Rego knowledge.
//
// This package implements [github.com/meigma/blob/registry.Policy] using pure Go
// for common SLSA provenance validation patterns.
//
// # Separate Module
//
// This package is a separate Go module (github.com/meigma/blob/policy/slsa)
// to isolate dependencies. Unlike the OPA package, this package has minimal
// dependencies and no Rego runtime.
//
// # How It Works
//
// The policies fetch in-toto attestations attached as OCI referrers and parse
// the SLSA provenance predicate using Go structs. Validation is performed
// directly in Go without policy evaluation engines.
//
// # Available Policies
//
// [RequireBuilder] verifies the build was performed by a specific builder:
//
//	policy := slsa.RequireBuilder("https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v2.0.0")
//
// [RequireSource] verifies the build source repository and optionally the ref:
//
//	policy := slsa.RequireSource("https://github.com/myorg/myrepo")
//	policy := slsa.RequireSource("https://github.com/myorg/myrepo",
//	    slsa.WithBranches("main"))
//
// [GitHubActionsWorkflow] combines builder and source validation for GitHub Actions:
//
//	policy, err := slsa.GitHubActionsWorkflow("myorg/myrepo",
//	    slsa.WithWorkflowBranches("main"),
//	    slsa.WithWorkflowTags("v*"))
//
// # Composition
//
// Use the policy package's composition utilities to combine SLSA policies
// with signature verification:
//
//	combined := policy.RequireAll(
//	    sigstorePolicy,  // Verify signature
//	    slsaPolicy,      // Then check provenance
//	)
//
// # Escape Hatch
//
// For complex validation logic not covered by these policies, use the OPA
// package which provides full Rego support.
package slsa
