// Package opa provides a client.Policy implementation for validating SLSA
// provenance attestations using OPA Rego policies.
//
// This package implements [github.com/meigma/blob/registry.Policy] using the
// Open Policy Agent (OPA) Rego language for flexible attestation validation.
//
// # Separate Module
//
// This package is a separate Go module (github.com/meigma/blob/policy/opa)
// to isolate the OPA dependency. This design allows users who don't need
// attestation validation to import github.com/meigma/blob without pulling in
// OPA and its transitive dependencies.
//
// # How It Works
//
// The Policy fetches in-toto attestations (SLSA provenance) attached as OCI
// referrers to the artifact being pulled. These attestations are parsed and
// passed to a user-defined Rego policy for evaluation.
//
// The evaluation flow:
//  1. Fetch referrers with artifact type "application/vnd.in-toto+json"
//  2. Parse attestations - unwrap DSSE envelope, extract in-toto statement
//  3. Filter by predicate type - accept SLSA provenance v1 and v0.2 by default
//  4. Build Rego input - manifest metadata + all matching attestations
//  5. Evaluate Rego - check data.blob.policy.allow / deny
//  6. Return result - nil on allow, error on deny
//
// # Rego Policy Format
//
// Policies must be defined in the data.blob.policy namespace and should
// define either an "allow" rule (boolean) or "deny" rules (set of strings).
//
// Example policy requiring builds from trusted GitHub Actions:
//
//	package blob.policy
//
//	import rego.v1
//
//	default allow := false
//
//	allow if {
//	    some att in input.attestations
//	    att.predicate.runDetails.builder.id == "https://github.com/actions/runner/github-hosted"
//	}
//
// Example policy with deny rules:
//
//	package blob.policy
//
//	import rego.v1
//
//	deny contains msg if {
//	    some att in input.attestations
//	    not startswith(att.predicate.buildDefinition.externalParameters.source.uri, "git+https://github.com/myorg/")
//	    msg := "source must be from myorg"
//	}
//
// # Input Structure
//
// The Rego input has the following structure:
//
//	{
//	    "manifest": {
//	        "reference": "ghcr.io/myorg/myarchive:v1",
//	        "digest": "sha256:abc123...",
//	        "mediaType": "application/vnd.oci.image.manifest.v1+json"
//	    },
//	    "attestations": [
//	        {
//	            "_type": "https://in-toto.io/Statement/v1",
//	            "predicateType": "https://slsa.dev/provenance/v1",
//	            "subject": [...],
//	            "predicate": {
//	                "buildDefinition": {...},
//	                "runDetails": {...}
//	            }
//	        }
//	    ]
//	}
//
// # Example Usage
//
//	policy, err := opa.NewPolicy(
//	    opa.WithPolicyFile("policy.rego"),
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
//	// Pull fails if SLSA provenance doesn't satisfy policy
//	archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
//
// # Signature Verification
//
// This package does NOT verify signatures on attestations. OPA evaluates the
// raw attestation content. For signature verification, use the sigstore policy
// package in combination with this one.
package opa
