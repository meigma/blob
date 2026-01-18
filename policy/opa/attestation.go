package opa

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/secure-systems-lab/go-securesystemslib/dsse"
)

// DSSEPayloadType is the expected payload type for in-toto statements.
const DSSEPayloadType = "application/vnd.in-toto+json"

// SigstoreBundleArtifactType is the OCI artifact type for Sigstore bundles.
const SigstoreBundleArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// inTotoStatement represents an in-toto statement structure.
type inTotoStatement struct {
	Type          string           `json:"_type"`
	PredicateType string           `json:"predicateType"`
	Subject       []inTotoSubject  `json:"subject"`
	Predicate     *json.RawMessage `json:"predicate"`
}

// inTotoSubject represents an in-toto subject.
type inTotoSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// sigstoreBundle represents a Sigstore bundle that wraps a DSSE envelope.
// This is the format used by GitHub's actions/attest-build-provenance action.
type sigstoreBundle struct {
	MediaType    string        `json:"mediaType"`
	DSSEEnvelope dsse.Envelope `json:"dsseEnvelope"`
}

// parseAttestation extracts an in-toto statement from either a DSSE envelope
// or a Sigstore bundle (which wraps a DSSE envelope).
// Returns the parsed attestation input or an error if parsing fails.
func parseAttestation(data []byte) (*AttestationInput, error) {
	// Try to parse as Sigstore bundle first
	var bundle sigstoreBundle
	if err := json.Unmarshal(data, &bundle); err == nil && bundle.DSSEEnvelope.Payload != "" {
		return parseDSSEEnvelope(&bundle.DSSEEnvelope)
	}

	// Fall back to plain DSSE envelope
	var envelope dsse.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: failed to parse DSSE envelope: %v", ErrInvalidAttestation, err)
	}

	return parseDSSEEnvelope(&envelope)
}

// parseDSSEEnvelope extracts an in-toto statement from a DSSE envelope.
func parseDSSEEnvelope(envelope *dsse.Envelope) (*AttestationInput, error) {
	// Validate payload type
	if envelope.PayloadType != DSSEPayloadType {
		return nil, fmt.Errorf("%w: unexpected payload type %q, expected %q",
			ErrInvalidAttestation, envelope.PayloadType, DSSEPayloadType)
	}

	// Decode base64 payload
	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode payload: %v", ErrInvalidAttestation, err)
	}

	// Parse the in-toto statement
	var stmt inTotoStatement
	if err := json.Unmarshal(payload, &stmt); err != nil {
		return nil, fmt.Errorf("%w: failed to parse in-toto statement: %v", ErrInvalidAttestation, err)
	}

	// Convert to AttestationInput
	att := &AttestationInput{
		Type:          stmt.Type,
		PredicateType: stmt.PredicateType,
		Subject:       make([]SubjectInput, len(stmt.Subject)),
	}

	for i, s := range stmt.Subject {
		att.Subject[i] = SubjectInput(s)
	}

	// Parse predicate as arbitrary JSON
	if stmt.Predicate != nil {
		if err := json.Unmarshal(*stmt.Predicate, &att.Predicate); err != nil {
			return nil, fmt.Errorf("%w: failed to parse predicate: %v", ErrInvalidAttestation, err)
		}
	}

	return att, nil
}

// matchesPredicateType checks if the attestation matches any of the accepted predicate types.
// If acceptedTypes is empty, all predicate types are accepted.
func matchesPredicateType(att *AttestationInput, acceptedTypes []string) bool {
	if len(acceptedTypes) == 0 {
		return true
	}
	return slices.Contains(acceptedTypes, att.PredicateType)
}

// ociManifest is a minimal OCI image manifest structure for parsing attestation layers.
type ociManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Layers        []ocispec.Descriptor `json:"layers"`
}

// parseOCIManifest parses an OCI image manifest to extract layer descriptors.
func parseOCIManifest(data []byte) (*ociManifest, error) {
	var manifest ociManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	// Validate it looks like an OCI manifest
	if manifest.SchemaVersion != 2 {
		return nil, fmt.Errorf("unexpected schema version: %d", manifest.SchemaVersion)
	}
	return &manifest, nil
}
