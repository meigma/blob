package opa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/open-policy-agent/opa/v1/rego"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/meigma/blob/client"
)

// DefaultArtifactType is the OCI artifact type for in-toto attestations.
const DefaultArtifactType = "application/vnd.in-toto+json"

// Default SLSA provenance predicate types.
var defaultPredicateTypes = []string{
	"https://slsa.dev/provenance/v1",
	"https://slsa.dev/provenance/v0.2",
}

// Policy implements client.Policy using OPA Rego for attestation validation.
// It fetches in-toto attestation referrers from the registry and evaluates
// them against a compiled Rego policy.
type Policy struct {
	query          *rego.PreparedEvalQuery
	artifactType   string
	predicateTypes []string
	logger         *slog.Logger
}

// NewPolicy creates an OPA-based attestation validation policy.
func NewPolicy(opts ...PolicyOption) (*Policy, error) {
	p := &Policy{
		artifactType:   DefaultArtifactType,
		predicateTypes: defaultPredicateTypes,
		logger:         slog.New(slog.DiscardHandler),
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, fmt.Errorf("opa: %w", err)
		}
	}

	if p.query == nil {
		return nil, ErrNoPolicy
	}

	return p, nil
}

// Evaluate implements client.Policy.
//
//nolint:gocritic // req passed by value per client.Policy interface contract
func (p *Policy) Evaluate(ctx context.Context, req client.PolicyRequest) error {
	// Fetch attestation referrers
	referrers, err := req.Client.Referrers(ctx, req.Ref, req.Subject, p.artifactType)
	if err != nil {
		if errors.Is(err, client.ErrReferrersUnsupported) {
			return errors.New("opa: registry does not support referrers API")
		}
		return fmt.Errorf("opa: list referrers: %w", err)
	}

	// Collect and parse attestations
	attestations := p.fetchAttestations(ctx, req, referrers)
	if len(attestations) == 0 {
		return ErrNoAttestations
	}

	// Build Rego input
	input := Input{
		Manifest: ManifestInput{
			Reference: req.Ref,
			Digest:    req.Digest,
			MediaType: req.Subject.MediaType,
		},
		Attestations: attestations,
	}

	// Evaluate policy
	return p.evaluatePolicy(ctx, input)
}

// fetchAttestations retrieves and parses attestations from referrers.
// For OCI image manifests (like Sigstore bundles), it fetches the layers containing
// the actual attestation content.
//
//nolint:gocritic // req passed by value per client.Policy interface contract
func (p *Policy) fetchAttestations(ctx context.Context, req client.PolicyRequest, referrers []ocispec.Descriptor) []AttestationInput {
	attestations := make([]AttestationInput, 0, len(referrers))

	for _, ref := range referrers {
		atts := p.fetchAttestationFromReferrer(ctx, req, ref)
		for _, att := range atts {
			if !matchesPredicateType(&att, p.predicateTypes) {
				p.logger.Debug("skipping attestation with non-matching predicate type",
					slog.String("predicate_type", att.PredicateType))
				continue
			}
			attestations = append(attestations, att)
		}
	}

	return attestations
}

// fetchAttestationFromReferrer fetches attestation content from a referrer descriptor.
// If the referrer is an OCI image manifest, it fetches the layers containing the attestation.
//
//nolint:gocritic // req passed by value per client.Policy interface contract
func (p *Policy) fetchAttestationFromReferrer(ctx context.Context, req client.PolicyRequest, ref ocispec.Descriptor) []AttestationInput {
	data, err := req.Client.FetchDescriptor(ctx, req.Ref, ref)
	if err != nil {
		p.logger.Warn("failed to fetch attestation descriptor",
			slog.String("digest", ref.Digest.String()),
			slog.Any("error", err))
		return nil
	}

	// Try to parse as an OCI manifest (for Sigstore bundles stored as OCI artifacts)
	manifest, err := parseOCIManifest(data)
	if err == nil && len(manifest.Layers) > 0 {
		return p.fetchAttestationsFromLayers(ctx, req, manifest.Layers)
	}

	// Fall back to parsing as direct attestation content
	att, err := parseAttestation(data)
	if err != nil {
		p.logger.Warn("failed to parse attestation",
			slog.String("digest", ref.Digest.String()),
			slog.Any("error", err))
		return nil
	}

	return []AttestationInput{*att}
}

// fetchAttestationsFromLayers fetches and parses attestations from OCI manifest layers.
//
//nolint:gocritic // req passed by value per client.Policy interface contract
func (p *Policy) fetchAttestationsFromLayers(ctx context.Context, req client.PolicyRequest, layers []ocispec.Descriptor) []AttestationInput {
	var attestations []AttestationInput

	for _, layer := range layers {
		data, err := req.Client.FetchDescriptor(ctx, req.Ref, layer)
		if err != nil {
			p.logger.Warn("failed to fetch attestation layer",
				slog.String("digest", layer.Digest.String()),
				slog.Any("error", err))
			continue
		}

		att, err := parseAttestation(data)
		if err != nil {
			p.logger.Warn("failed to parse attestation layer",
				slog.String("digest", layer.Digest.String()),
				slog.Any("error", err))
			continue
		}

		attestations = append(attestations, *att)
	}

	return attestations
}

// evaluatePolicy runs the Rego policy against the input.
func (p *Policy) evaluatePolicy(ctx context.Context, input Input) error {
	results, err := p.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPolicyEvaluation, err)
	}

	if len(results) == 0 {
		return fmt.Errorf("%w: no results from policy evaluation", ErrPolicyEvaluation)
	}

	// Extract the policy result
	result, ok := results[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: unexpected result type", ErrPolicyEvaluation)
	}

	return checkPolicyResult(result)
}

// checkPolicyResult evaluates the allow/deny rules from the Rego result.
func checkPolicyResult(result map[string]any) error {
	// Check for explicit deny rules first
	if err := checkDenyRules(result); err != nil {
		return err
	}

	// Check for allow rule
	if allow, ok := result["allow"]; ok {
		if allowed, ok := allow.(bool); ok && allowed {
			return nil
		}
	}

	// Default deny if allow is false or not set
	return ErrPolicyDenied
}

// checkDenyRules checks for explicit deny rules in the policy result.
func checkDenyRules(result map[string]any) error {
	deny, ok := result["deny"]
	if !ok {
		return nil
	}

	denySet, ok := deny.([]any)
	if !ok || len(denySet) == 0 {
		return nil
	}

	// Collect denial reasons
	reasons := collectDenyReasons(denySet)
	if len(reasons) > 0 {
		return fmt.Errorf("%w: %v", ErrPolicyDenied, reasons)
	}
	return ErrPolicyDenied
}

// collectDenyReasons extracts string reasons from the deny set.
func collectDenyReasons(denySet []any) []string {
	var reasons []string
	for _, d := range denySet {
		if reason, ok := d.(string); ok {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

// Ensure Policy implements client.Policy.
var _ client.Policy = (*Policy)(nil)
