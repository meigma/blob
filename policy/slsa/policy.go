package slsa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/meigma/blob/registry"
)

// Policy implements registry.Policy for SLSA provenance validation.
type Policy struct {
	validators    []provenanceValidator
	artifactTypes []string
	logger        *slog.Logger
}

// provenanceValidator validates a single provenance attestation.
type provenanceValidator func(*Provenance) error

// NewPolicy creates an SLSA provenance policy with the given options.
func NewPolicy(opts ...PolicyOption) (*Policy, error) {
	p := &Policy{
		artifactTypes: []string{InTotoArtifactType, SigstoreBundleArtifactType},
		logger:        slog.New(slog.DiscardHandler),
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, fmt.Errorf("slsa: %w", err)
		}
	}

	if len(p.validators) == 0 {
		return nil, ErrNoValidators
	}

	return p, nil
}

// Evaluate implements registry.Policy.
//
//nolint:gocritic // req passed by value per interface contract
func (p *Policy) Evaluate(ctx context.Context, req registry.PolicyRequest) error {
	// Fetch attestations
	provenances, err := p.fetchProvenances(ctx, req)
	if err != nil {
		return err
	}

	if len(provenances) == 0 {
		return ErrNoAttestations
	}

	// Try to find at least one valid provenance
	var lastErr error
	for _, prov := range provenances {
		if err := p.validateProvenance(prov); err != nil {
			lastErr = err
			continue
		}
		return nil // Found a valid one
	}

	return lastErr
}

//nolint:gocritic // req passed by value per interface contract
func (p *Policy) fetchProvenances(ctx context.Context, req registry.PolicyRequest) ([]*Provenance, error) {
	var provenances []*Provenance

	for _, artifactType := range p.artifactTypes {
		referrers, err := req.Client.Referrers(ctx, req.Ref, req.Subject, artifactType)
		if err != nil {
			if errors.Is(err, registry.ErrReferrersUnsupported) {
				return nil, errors.New("slsa: registry does not support referrers API")
			}
			p.logger.Debug("failed to list referrers",
				slog.String("artifact_type", artifactType),
				slog.Any("error", err))
			continue
		}

		for _, ref := range referrers {
			prov := p.fetchProvenance(ctx, req, ref)
			if prov != nil {
				provenances = append(provenances, prov)
			}
		}
	}

	return provenances, nil
}

//nolint:gocritic // req passed by value per interface contract
func (p *Policy) fetchProvenance(ctx context.Context, req registry.PolicyRequest, ref ocispec.Descriptor) *Provenance {
	data, err := req.Client.FetchDescriptor(ctx, req.Ref, ref)
	if err != nil {
		p.logger.Debug("failed to fetch attestation",
			slog.String("digest", ref.Digest.String()),
			slog.Any("error", err))
		return nil
	}

	// Try to parse as OCI manifest first (may have layers)
	manifest, err := parseOCIManifest(data)
	if err == nil && len(manifest.Layers) > 0 {
		for _, layer := range manifest.Layers {
			layerData, err := req.Client.FetchDescriptor(ctx, req.Ref, layer)
			if err != nil {
				continue
			}
			prov, err := ParseProvenance(layerData)
			if err == nil {
				return prov
			}
		}
		return nil
	}

	// Try direct parsing
	prov, err := ParseProvenance(data)
	if err != nil {
		p.logger.Debug("failed to parse provenance",
			slog.String("digest", ref.Digest.String()),
			slog.Any("error", err))
		return nil
	}

	return prov
}

func (p *Policy) validateProvenance(prov *Provenance) error {
	for _, v := range p.validators {
		if err := v(prov); err != nil {
			return err
		}
	}
	return nil
}

// ociManifest is a minimal OCI manifest for parsing layers.
type ociManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Layers        []ocispec.Descriptor `json:"layers"`
}

func parseOCIManifest(data []byte) (*ociManifest, error) {
	var m ociManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 2 {
		return nil, fmt.Errorf("unexpected schema version: %d", m.SchemaVersion)
	}
	return &m, nil
}

// RequireBuilder creates a policy requiring a specific builder ID.
//
// The builderID should match exactly the builder.id field in the SLSA provenance.
// For GitHub Actions with slsa-github-generator, this looks like:
//
//	"https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v2.0.0"
func RequireBuilder(builderID string) *Policy {
	p, _ := NewPolicy(withBuilderValidator(builderID))
	return p
}

// RequireSource creates a policy requiring a specific source repository.
//
// The repo should be the repository URL prefix to match. For GitHub repositories:
//
//	"https://github.com/myorg/myrepo"
//
// Use options to further constrain which branches or tags are allowed.
func RequireSource(repo string, opts ...SourceOption) *Policy {
	cfg := &sourceConfig{repo: repo}
	for _, opt := range opts {
		opt(cfg)
	}
	p, _ := NewPolicy(withSourceValidator(cfg))
	return p
}

// GitHubActionsWorkflow creates a policy for GitHub Actions workflows.
//
// This validates that:
//   - The build was run by a GitHub Actions workflow
//   - The source repository matches "https://github.com/{repo}"
//   - The workflow path matches (if specified via WithWorkflowPath)
//   - The git ref matches allowed patterns (if specified via WithWorkflowBranches/Tags)
//
// Example:
//
//	policy, err := slsa.GitHubActionsWorkflow("myorg/myrepo",
//	    slsa.WithWorkflowPath(".github/workflows/release.yml"),
//	    slsa.WithWorkflowBranches("main"),
//	    slsa.WithWorkflowTags("v*"),
//	)
func GitHubActionsWorkflow(repo string, opts ...GitHubActionsWorkflowOption) (*Policy, error) {
	if repo == "" {
		return nil, errors.New("slsa: repository cannot be empty")
	}

	cfg := &ghActionsConfig{repo: repo}
	for _, opt := range opts {
		opt(cfg)
	}

	return NewPolicy(withGitHubActionsValidator(cfg))
}

// --- Internal validators ---

func withBuilderValidator(builderID string) PolicyOption {
	return func(p *Policy) error {
		p.validators = append(p.validators, func(prov *Provenance) error {
			if prov.BuilderID != builderID {
				return fmt.Errorf("%w: got %q, want %q",
					ErrBuilderMismatch, prov.BuilderID, builderID)
			}
			return nil
		})
		return nil
	}
}

func withSourceValidator(cfg *sourceConfig) PolicyOption {
	return func(p *Policy) error {
		p.validators = append(p.validators, func(prov *Provenance) error {
			if !strings.HasPrefix(prov.SourceRepo, cfg.repo) {
				return fmt.Errorf("%w: got %q, want prefix %q",
					ErrSourceMismatch, prov.SourceRepo, cfg.repo)
			}

			if cfg.ref != "" && prov.SourceRef != cfg.ref {
				return fmt.Errorf("%w: got %q, want %q",
					ErrRefMismatch, prov.SourceRef, cfg.ref)
			}

			if len(cfg.refPatterns) > 0 {
				matched := false
				for _, pattern := range cfg.refPatterns {
					if pattern.MatchString(prov.SourceRef) {
						matched = true
						break
					}
				}
				if !matched {
					return fmt.Errorf("%w: %q does not match allowed patterns",
						ErrRefMismatch, prov.SourceRef)
				}
			}

			return nil
		})
		return nil
	}
}

func withGitHubActionsValidator(cfg *ghActionsConfig) PolicyOption {
	return func(p *Policy) error {
		p.validators = append(p.validators, func(prov *Provenance) error {
			// Verify repository
			expectedRepo := "https://github.com/" + cfg.repo
			if !strings.HasPrefix(prov.SourceRepo, expectedRepo) {
				return fmt.Errorf("%w: got %q, want prefix %q",
					ErrSourceMismatch, prov.SourceRepo, expectedRepo)
			}

			// Verify workflow path if specified
			if cfg.workflowPath != "" && prov.WorkflowPath != cfg.workflowPath {
				return fmt.Errorf("%w: got %q, want %q",
					ErrWorkflowMismatch, prov.WorkflowPath, cfg.workflowPath)
			}

			// Verify ref patterns if specified
			if len(cfg.refPatterns) > 0 {
				matched := false
				for _, pattern := range cfg.refPatterns {
					if pattern.MatchString(prov.SourceRef) {
						matched = true
						break
					}
				}
				if !matched {
					return fmt.Errorf("%w: %q does not match allowed patterns",
						ErrRefMismatch, prov.SourceRef)
				}
			}

			return nil
		})
		return nil
	}
}

var _ registry.Policy = (*Policy)(nil)
