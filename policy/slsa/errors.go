package slsa

import "errors"

// Sentinel errors for SLSA policy validation.
var (
	// ErrNoAttestations indicates no matching attestations were found.
	ErrNoAttestations = errors.New("slsa: no attestations found")

	// ErrInvalidProvenance indicates the provenance predicate is malformed.
	ErrInvalidProvenance = errors.New("slsa: invalid provenance format")

	// ErrBuilderMismatch indicates the builder ID doesn't match the requirement.
	ErrBuilderMismatch = errors.New("slsa: builder mismatch")

	// ErrSourceMismatch indicates the source repository doesn't match.
	ErrSourceMismatch = errors.New("slsa: source repository mismatch")

	// ErrRefMismatch indicates the git ref doesn't match allowed refs.
	ErrRefMismatch = errors.New("slsa: ref mismatch")

	// ErrWorkflowMismatch indicates the workflow path doesn't match.
	ErrWorkflowMismatch = errors.New("slsa: workflow path mismatch")

	// ErrNoValidators indicates no validators were configured.
	ErrNoValidators = errors.New("slsa: at least one validator required")
)
