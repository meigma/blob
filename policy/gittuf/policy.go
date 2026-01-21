package gittuf

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	verifyopts "github.com/gittuf/gittuf/experimental/gittuf/options/verify"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/meigma/blob/registry"
)

// InTotoArtifactType is the OCI artifact type for in-toto attestations.
const InTotoArtifactType = "application/vnd.in-toto+json"

// SigstoreBundleArtifactType is the OCI artifact type for Sigstore bundles.
const SigstoreBundleArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// DSSEPayloadType is the expected payload type for in-toto statements.
const DSSEPayloadType = "application/vnd.in-toto+json"

// SLSAPredicateTypes are the supported SLSA provenance predicate types.
var SLSAPredicateTypes = []string{
	"https://slsa.dev/provenance/v1",
	"https://slsa.dev/provenance/v0.2",
}

// Policy implements registry.Policy for gittuf source provenance verification.
type Policy struct {
	repoURL string
	cache   *RepositoryCache
	logger  *slog.Logger

	// Verification options
	latestOnly  bool
	overrideRef string

	// Graceful degradation
	allowMissingGittuf     bool
	allowMissingProvenance bool
}

// NewPolicy creates a gittuf source provenance policy.
func NewPolicy(opts ...PolicyOption) (*Policy, error) {
	p := &Policy{
		cache:      DefaultCache(),
		logger:     slog.New(slog.DiscardHandler),
		latestOnly: true, // Faster verification by default
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, fmt.Errorf("gittuf: %w", err)
		}
	}

	if p.repoURL == "" {
		return nil, ErrNoRepository
	}

	return p, nil
}

// GitHubRepository creates a policy for a GitHub-hosted repository.
// This is a convenience constructor that sets the repository URL.
func GitHubRepository(owner, repo string, opts ...PolicyOption) (*Policy, error) {
	url := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	return NewPolicy(append([]PolicyOption{WithRepository(url)}, opts...)...)
}

// Evaluate implements registry.Policy.
//
//nolint:gocritic // req passed by value per interface contract
func (p *Policy) Evaluate(ctx context.Context, req registry.PolicyRequest) error {
	p.logger.Debug("starting gittuf verification",
		slog.String("ref", req.Ref),
		slog.String("digest", req.Digest))

	// 1. Extract source information from SLSA provenance
	sourceInfo, err := p.extractSourceInfo(ctx, req)
	if err != nil {
		if p.allowMissingProvenance && errors.Is(err, ErrNoSLSAProvenance) {
			p.logger.Warn("no SLSA provenance found, skipping gittuf verification")
			return nil
		}
		return err
	}

	p.logger.Debug("extracted source info",
		slog.String("repo", sourceInfo.Repo),
		slog.String("ref", sourceInfo.Ref),
		slog.String("commit", sourceInfo.Commit))

	// 2. Get or clone repository
	repo, err := p.cache.Get(ctx, p.repoURL)
	if err != nil {
		// Clone can fail if the repository doesn't have valid gittuf metadata.
		// When allowMissingGittuf is enabled, treat clone failures as "no gittuf".
		if p.allowMissingGittuf {
			p.logger.Warn("failed to clone/load gittuf repository, skipping verification",
				slog.String("repo", p.repoURL),
				slog.Any("error", err))
			return nil
		}
		return fmt.Errorf("%w: %v", ErrCloneFailed, err)
	}

	// 3. Check if repository has gittuf enabled
	hasPolicy, err := repo.HasPolicy()
	if err != nil {
		return fmt.Errorf("gittuf: failed to check for policy: %w", err)
	}
	if !hasPolicy {
		if p.allowMissingGittuf {
			p.logger.Warn("repository does not have gittuf enabled, skipping verification",
				slog.String("repo", p.repoURL))
			return nil
		}
		return ErrNoGittufPolicy
	}

	// 4. Refresh RSL from remote
	if err := repo.PullRSL("origin"); err != nil {
		// Log warning but continue with cached RSL
		p.logger.Warn("failed to pull RSL, using cached version",
			slog.Any("error", err))
	}

	// 5. Determine which ref to verify
	refToVerify := sourceInfo.Ref
	if p.overrideRef != "" {
		refToVerify = p.overrideRef
	}
	if refToVerify == "" {
		return ErrNoRefToVerify
	}

	// 6. Build verify options
	var verifyOpts []verifyopts.Option
	if p.latestOnly {
		verifyOpts = append(verifyOpts, verifyopts.WithLatestOnly())
	}

	// 7. Verify the ref
	p.logger.Debug("verifying ref",
		slog.String("ref", refToVerify),
		slog.Bool("latest_only", p.latestOnly))

	if err := repo.VerifyRef(ctx, refToVerify, verifyOpts...); err != nil {
		return fmt.Errorf("%w: %v", ErrVerificationFailed, err)
	}

	p.logger.Debug("gittuf verification successful",
		slog.String("ref", refToVerify))

	return nil
}

// sourceInfo contains extracted source provenance information.
type sourceInfo struct {
	Repo   string // Source repository URL
	Ref    string // Git ref (e.g., refs/heads/main, refs/tags/v1.0.0)
	Commit string // Git commit SHA
}

// extractSourceInfo retrieves source information from SLSA provenance.
//
//nolint:gocritic // req passed by value per interface contract
func (p *Policy) extractSourceInfo(ctx context.Context, req registry.PolicyRequest) (*sourceInfo, error) {
	// Try both artifact types
	artifactTypes := []string{InTotoArtifactType, SigstoreBundleArtifactType}

	for _, artifactType := range artifactTypes {
		referrers, err := req.Client.Referrers(ctx, req.Ref, req.Subject, artifactType)
		if err != nil {
			if errors.Is(err, registry.ErrReferrersUnsupported) {
				return nil, fmt.Errorf("gittuf: registry does not support referrers API: %w", err)
			}
			p.logger.Debug("failed to list referrers",
				slog.String("artifact_type", artifactType),
				slog.Any("error", err))
			continue
		}

		for _, ref := range referrers {
			info := p.tryExtractSourceInfo(ctx, req, ref)
			if info != nil {
				return info, nil
			}
		}
	}

	return nil, ErrNoSLSAProvenance
}

// tryExtractSourceInfo attempts to extract source info from a single attestation.
//
//nolint:gocritic // req passed by value per interface contract
func (p *Policy) tryExtractSourceInfo(ctx context.Context, req registry.PolicyRequest, ref ocispec.Descriptor) *sourceInfo {
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
			info := p.parseSourceInfo(layerData)
			if info != nil {
				return info
			}
		}
		return nil
	}

	// Try direct parsing
	return p.parseSourceInfo(data)
}

// parseSourceInfo parses source information from attestation data.
func (p *Policy) parseSourceInfo(data []byte) *sourceInfo {
	// Try Sigstore bundle first
	var bundle sigstoreBundle
	if err := json.Unmarshal(data, &bundle); err == nil && bundle.DSSEEnvelope.Payload != "" {
		return p.parseFromDSSE(&bundle.DSSEEnvelope)
	}

	// Try raw DSSE envelope
	var envelope dsseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}

	return p.parseFromDSSE(&envelope)
}

// parseFromDSSE extracts source info from a DSSE envelope.
func (p *Policy) parseFromDSSE(envelope *dsseEnvelope) *sourceInfo {
	if envelope.PayloadType != DSSEPayloadType {
		return nil
	}

	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return nil
	}

	var stmt inTotoStatement
	if err := json.Unmarshal(payload, &stmt); err != nil {
		return nil
	}

	// Check predicate type
	if !isSLSAPredicateType(stmt.PredicateType) {
		return nil
	}

	// Parse predicate
	var predicate map[string]any
	if stmt.Predicate != nil {
		if err := json.Unmarshal(*stmt.Predicate, &predicate); err != nil {
			return nil
		}
	}

	return extractSourceFromPredicate(stmt.PredicateType, predicate)
}

// --- Helper types for parsing attestations ---

type sigstoreBundle struct {
	MediaType    string       `json:"mediaType"`
	DSSEEnvelope dsseEnvelope `json:"dsseEnvelope"`
}

type dsseEnvelope struct {
	PayloadType string `json:"payloadType"`
	Payload     string `json:"payload"`
}

type inTotoStatement struct {
	Type          string           `json:"_type"`
	PredicateType string           `json:"predicateType"`
	Predicate     *json.RawMessage `json:"predicate"`
}

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

func isSLSAPredicateType(pt string) bool {
	for _, valid := range SLSAPredicateTypes {
		if pt == valid {
			return true
		}
	}
	return false
}

func extractSourceFromPredicate(predicateType string, predicate map[string]any) *sourceInfo {
	info := &sourceInfo{}

	if predicateType == "https://slsa.dev/provenance/v1" {
		extractSLSAv1(info, predicate)
	} else {
		extractSLSAv02(info, predicate)
	}

	// Only return if we found at least the repo
	if info.Repo == "" {
		return nil
	}
	return info
}

// extractSLSAv1 extracts source info from SLSA provenance v1 format.
func extractSLSAv1(info *sourceInfo, predicate map[string]any) {
	buildDef := getMap(predicate, "buildDefinition")
	if buildDef == nil {
		return
	}

	// GitHub Actions format: workflow in externalParameters
	extParams := getMap(buildDef, "externalParameters")
	workflow := getMap(extParams, "workflow")
	if repo := getString(workflow, "repository"); repo != "" {
		info.Repo = repo
	}
	if ref := getString(workflow, "ref"); ref != "" {
		info.Ref = ref
	}

	// Resolved dependencies for source commit
	extractFromResolvedDeps(info, buildDef)
}

// extractFromResolvedDeps extracts source info from SLSA v1 resolved dependencies.
func extractFromResolvedDeps(info *sourceInfo, buildDef map[string]any) {
	resolvedDeps, ok := buildDef["resolvedDependencies"].([]any)
	if !ok {
		return
	}
	for _, dep := range resolvedDeps {
		depMap, ok := dep.(map[string]any)
		if !ok {
			continue
		}
		if info.Repo == "" {
			info.Repo = getString(depMap, "uri")
		}
		if digest := getMap(depMap, "digest"); digest != nil {
			if sha := getString(digest, "gitCommit"); sha != "" {
				info.Commit = sha
			}
		}
	}
}

// extractSLSAv02 extracts source info from SLSA provenance v0.2 format.
func extractSLSAv02(info *sourceInfo, predicate map[string]any) {
	// Invocation fields
	invocation := getMap(predicate, "invocation")
	configSource := getMap(invocation, "configSource")
	if uri := getString(configSource, "uri"); uri != "" {
		info.Repo = uri
	}
	if digest := getMap(configSource, "digest"); digest != nil {
		info.Commit = getString(digest, "sha1")
	}

	// Materials for source info
	extractFromMaterials(info, predicate)
}

// extractFromMaterials extracts source info from SLSA v0.2 materials.
func extractFromMaterials(info *sourceInfo, predicate map[string]any) {
	materials, ok := predicate["materials"].([]any)
	if !ok {
		return
	}
	for _, mat := range materials {
		matMap, ok := mat.(map[string]any)
		if !ok {
			continue
		}
		if info.Repo == "" {
			info.Repo = getString(matMap, "uri")
		}
		if digest := getMap(matMap, "digest"); digest != nil {
			if sha := getString(digest, "sha1"); sha != "" && info.Commit == "" {
				info.Commit = sha
			}
		}
	}
}

// getMap retrieves a nested map from a parent map, returning nil if not found.
func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

// getString retrieves a string value from a map, returning empty string if not found.
func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

var _ registry.Policy = (*Policy)(nil)
