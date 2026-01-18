package opa

// Input is the top-level structure passed to Rego policies.
type Input struct {
	// Manifest contains metadata about the OCI manifest being evaluated.
	Manifest ManifestInput `json:"manifest"`

	// Attestations contains all parsed in-toto attestations found as referrers.
	Attestations []AttestationInput `json:"attestations"`
}

// ManifestInput contains OCI manifest metadata.
type ManifestInput struct {
	// Reference is the original OCI reference used to pull the artifact.
	Reference string `json:"reference"`

	// Digest is the manifest digest (e.g., "sha256:abc123...").
	Digest string `json:"digest"`

	// MediaType is the manifest media type.
	MediaType string `json:"mediaType"`
}

// AttestationInput represents a parsed in-toto statement.
type AttestationInput struct {
	// Type is the in-toto statement type (typically "_type": "https://in-toto.io/Statement/v1").
	Type string `json:"_type"`

	// PredicateType identifies the attestation type (e.g., "https://slsa.dev/provenance/v1").
	PredicateType string `json:"predicateType"`

	// Subject contains the artifacts this attestation applies to.
	Subject []SubjectInput `json:"subject"`

	// Predicate contains the attestation-specific data (e.g., SLSA provenance).
	Predicate map[string]any `json:"predicate"`
}

// SubjectInput represents an in-toto subject.
type SubjectInput struct {
	// Name is the subject identifier.
	Name string `json:"name"`

	// Digest maps algorithm names to digest values.
	Digest map[string]string `json:"digest"`
}
