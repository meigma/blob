package slsa

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
)

// SLSAPredicateTypes are the supported SLSA provenance predicate types.
var SLSAPredicateTypes = []string{
	"https://slsa.dev/provenance/v1",
	"https://slsa.dev/provenance/v0.2",
}

// DSSEPayloadType is the expected payload type for in-toto statements.
const DSSEPayloadType = "application/vnd.in-toto+json"

// InTotoArtifactType is the OCI artifact type for in-toto attestations.
const InTotoArtifactType = "application/vnd.in-toto+json"

// SigstoreBundleArtifactType is the OCI artifact type for Sigstore bundles.
const SigstoreBundleArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// Provenance represents a parsed SLSA provenance attestation.
type Provenance struct {
	// PredicateType is the SLSA predicate type (v1 or v0.2).
	PredicateType string

	// BuilderID is the builder identifier.
	// For SLSA v1: from runDetails.builder.id
	// For SLSA v0.2: from builder.id
	BuilderID string

	// BuildType is the build type from buildDefinition.buildType (v1 only).
	BuildType string

	// SourceRepo is the source repository URL.
	SourceRepo string

	// SourceRef is the git ref (branch, tag, or commit).
	SourceRef string

	// SourceDigest is the source commit SHA.
	SourceDigest string

	// WorkflowPath is the workflow file path (for GitHub Actions).
	WorkflowPath string

	// Raw contains the full predicate for advanced inspection.
	Raw map[string]any
}

// inTotoStatement represents an in-toto statement.
type inTotoStatement struct {
	Type          string           `json:"_type"`
	PredicateType string           `json:"predicateType"`
	Subject       []inTotoSubject  `json:"subject"`
	Predicate     *json.RawMessage `json:"predicate"`
}

type inTotoSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// sigstoreBundle wraps a DSSE envelope in Sigstore bundle format.
type sigstoreBundle struct {
	MediaType    string        `json:"mediaType"`
	DSSEEnvelope dsse.Envelope `json:"dsseEnvelope"`
}

// ParseProvenance extracts SLSA provenance from attestation data.
// Supports both raw DSSE envelopes and Sigstore bundles.
func ParseProvenance(data []byte) (*Provenance, error) {
	// Try Sigstore bundle first
	var bundle sigstoreBundle
	if err := json.Unmarshal(data, &bundle); err == nil && bundle.DSSEEnvelope.Payload != "" {
		return parseFromDSSE(&bundle.DSSEEnvelope)
	}

	// Try raw DSSE envelope
	var envelope dsse.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProvenance, err)
	}

	return parseFromDSSE(&envelope)
}

func parseFromDSSE(envelope *dsse.Envelope) (*Provenance, error) {
	if envelope.PayloadType != DSSEPayloadType {
		return nil, fmt.Errorf("%w: unexpected payload type %q",
			ErrInvalidProvenance, envelope.PayloadType)
	}

	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %v", ErrInvalidProvenance, err)
	}

	var stmt inTotoStatement
	if err := json.Unmarshal(payload, &stmt); err != nil {
		return nil, fmt.Errorf("%w: parse statement: %v", ErrInvalidProvenance, err)
	}

	// Check predicate type
	if !isSLSAPredicateType(stmt.PredicateType) {
		return nil, fmt.Errorf("%w: unsupported predicate type %q",
			ErrInvalidProvenance, stmt.PredicateType)
	}

	// Parse predicate
	var predicate map[string]any
	if stmt.Predicate != nil {
		if err := json.Unmarshal(*stmt.Predicate, &predicate); err != nil {
			return nil, fmt.Errorf("%w: parse predicate: %v", ErrInvalidProvenance, err)
		}
	}

	return extractProvenance(stmt.PredicateType, predicate)
}

func isSLSAPredicateType(pt string) bool {
	for _, valid := range SLSAPredicateTypes {
		if pt == valid {
			return true
		}
	}
	return false
}

func extractProvenance(predicateType string, predicate map[string]any) (*Provenance, error) {
	prov := &Provenance{
		PredicateType: predicateType,
		Raw:           predicate,
	}

	// SLSA v1 format
	if predicateType == "https://slsa.dev/provenance/v1" {
		extractSLSAv1(prov, predicate)
	} else {
		// SLSA v0.2 format
		extractSLSAv02(prov, predicate)
	}

	return prov, nil
}

// extractSLSAv1 extracts fields from SLSA provenance v1 format.
func extractSLSAv1(prov *Provenance, predicate map[string]any) {
	// Builder ID: runDetails.builder.id
	if runDetails, ok := predicate["runDetails"].(map[string]any); ok {
		if builder, ok := runDetails["builder"].(map[string]any); ok {
			prov.BuilderID, _ = builder["id"].(string)
		}
	}

	// Build definition fields
	if buildDef, ok := predicate["buildDefinition"].(map[string]any); ok {
		prov.BuildType, _ = buildDef["buildType"].(string)

		if extParams, ok := buildDef["externalParameters"].(map[string]any); ok {
			// GitHub Actions format
			if workflow, ok := extParams["workflow"].(map[string]any); ok {
				prov.SourceRepo, _ = workflow["repository"].(string)
				prov.SourceRef, _ = workflow["ref"].(string)
				prov.WorkflowPath, _ = workflow["path"].(string)
			}
		}

		// Resolved dependencies for source info
		if resolvedDeps, ok := buildDef["resolvedDependencies"].([]any); ok {
			for _, dep := range resolvedDeps {
				if depMap, ok := dep.(map[string]any); ok {
					if uri, ok := depMap["uri"].(string); ok && prov.SourceRepo == "" {
						prov.SourceRepo = uri
					}
					if digest, ok := depMap["digest"].(map[string]any); ok {
						if sha, ok := digest["gitCommit"].(string); ok {
							prov.SourceDigest = sha
						}
					}
				}
			}
		}
	}
}

// extractSLSAv02 extracts fields from SLSA provenance v0.2 format.
func extractSLSAv02(prov *Provenance, predicate map[string]any) {
	// Builder ID: builder.id
	if builder, ok := predicate["builder"].(map[string]any); ok {
		prov.BuilderID, _ = builder["id"].(string)
	}

	// Invocation fields
	if invocation, ok := predicate["invocation"].(map[string]any); ok {
		if configSource, ok := invocation["configSource"].(map[string]any); ok {
			prov.SourceRepo, _ = configSource["uri"].(string)
			if digest, ok := configSource["digest"].(map[string]any); ok {
				prov.SourceDigest, _ = digest["sha1"].(string)
			}
			prov.WorkflowPath, _ = configSource["entryPoint"].(string)
		}
	}

	// Materials for source info
	if materials, ok := predicate["materials"].([]any); ok {
		for _, mat := range materials {
			if matMap, ok := mat.(map[string]any); ok {
				if uri, ok := matMap["uri"].(string); ok && prov.SourceRepo == "" {
					prov.SourceRepo = uri
				}
				if digest, ok := matMap["digest"].(map[string]any); ok {
					if sha, ok := digest["sha1"].(string); ok && prov.SourceDigest == "" {
						prov.SourceDigest = sha
					}
				}
			}
		}
	}
}
