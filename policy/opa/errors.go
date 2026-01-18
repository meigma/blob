package opa

import "errors"

var (
	// ErrNoPolicy indicates that no Rego policy was provided.
	ErrNoPolicy = errors.New("opa: no policy provided")

	// ErrNoAttestations indicates that no matching attestations were found.
	ErrNoAttestations = errors.New("opa: no attestations found for manifest")

	// ErrPolicyDenied indicates that the policy evaluation returned deny.
	ErrPolicyDenied = errors.New("opa: policy denied")

	// ErrPolicyEvaluation indicates a failure during policy evaluation.
	ErrPolicyEvaluation = errors.New("opa: policy evaluation failed")

	// ErrInvalidAttestation indicates an attestation could not be parsed.
	ErrInvalidAttestation = errors.New("opa: invalid attestation format")
)
