package slsa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/registry"
)

// mockPolicyClient implements registry.PolicyClient for testing.
type mockPolicyClient struct {
	referrers   []ocispec.Descriptor
	referrerErr error
	descriptors map[string][]byte
	fetchErr    error
}

//nolint:gocritic // implements registry.PolicyClient interface
func (m *mockPolicyClient) Referrers(_ context.Context, _ string, _ ocispec.Descriptor, _ string) ([]ocispec.Descriptor, error) {
	if m.referrerErr != nil {
		return nil, m.referrerErr
	}
	return m.referrers, nil
}

//nolint:gocritic // implements registry.PolicyClient interface
func (m *mockPolicyClient) FetchDescriptor(_ context.Context, _ string, desc ocispec.Descriptor) ([]byte, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if m.descriptors != nil {
		if data, ok := m.descriptors[desc.Digest.String()]; ok {
			return data, nil
		}
	}
	return nil, registry.ErrNotFound
}

// createDSSEEnvelope creates a DSSE envelope for testing.
func createDSSEEnvelope(statement any) []byte {
	payload, _ := json.Marshal(statement)
	envelope := map[string]any{
		"payloadType": "application/vnd.in-toto+json",
		"payload":     base64.StdEncoding.EncodeToString(payload),
		"signatures":  []any{},
	}
	data, _ := json.Marshal(envelope)
	return data
}

// createSLSAv1Statement creates an in-toto statement with SLSA v1 provenance.
func createSLSAv1Statement(builderID, sourceRepo, sourceRef, workflowPath string) map[string]any {
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": "https://slsa.dev/provenance/v1",
		"subject": []any{
			map[string]any{
				"name": "test-artifact",
				"digest": map[string]any{
					"sha256": "abc123",
				},
			},
		},
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": "https://slsa.dev/github-actions-workflow/v1",
				"externalParameters": map[string]any{
					"workflow": map[string]any{
						"repository": sourceRepo,
						"ref":        sourceRef,
						"path":       workflowPath,
					},
				},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{
					"id": builderID,
				},
			},
		},
	}
	return statement
}

func TestNewPolicy_NoValidators(t *testing.T) {
	t.Parallel()

	_, err := NewPolicy()
	require.ErrorIs(t, err, ErrNoValidators)
}

func TestRequireBuilder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		wantBuilderID string
		gotBuilderID  string
		wantErr       error
	}{
		{
			name:          "matches builder",
			wantBuilderID: "https://github.com/slsa-framework/slsa-github-generator",
			gotBuilderID:  "https://github.com/slsa-framework/slsa-github-generator",
			wantErr:       nil,
		},
		{
			name:          "builder mismatch",
			wantBuilderID: "https://github.com/slsa-framework/slsa-github-generator",
			gotBuilderID:  "https://github.com/other/builder",
			wantErr:       ErrBuilderMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := RequireBuilder(tt.wantBuilderID)
			require.NotNil(t, policy)

			attDigest := digest.FromString("attestation")
			manifestDigest := digest.FromString("manifest")

			statement := createSLSAv1Statement(tt.gotBuilderID, "https://github.com/myorg/myrepo", "refs/heads/main", ".github/workflows/build.yml")
			envelope := createDSSEEnvelope(statement)

			mockClient := &mockPolicyClient{
				referrers: []ocispec.Descriptor{
					{
						MediaType:    InTotoArtifactType,
						Digest:       attDigest,
						Size:         int64(len(envelope)),
						ArtifactType: InTotoArtifactType,
					},
				},
				descriptors: map[string][]byte{
					attDigest.String(): envelope,
				},
			}

			req := registry.PolicyRequest{
				Ref:    "example.com/repo:tag",
				Digest: manifestDigest.String(),
				Subject: ocispec.Descriptor{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    manifestDigest,
					Size:      100,
				},
				Client: mockClient,
			}

			err := policy.Evaluate(context.Background(), req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRequireSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		wantRepo string
		opts     []SourceOption
		gotRepo  string
		gotRef   string
		wantErr  error
	}{
		{
			name:     "matches repo prefix",
			wantRepo: "https://github.com/myorg/myrepo",
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/heads/main",
			wantErr:  nil,
		},
		{
			name:     "repo mismatch",
			wantRepo: "https://github.com/myorg/myrepo",
			gotRepo:  "https://github.com/other/repo",
			gotRef:   "refs/heads/main",
			wantErr:  ErrSourceMismatch,
		},
		{
			name:     "with exact ref - matches",
			wantRepo: "https://github.com/myorg/myrepo",
			opts:     []SourceOption{WithRef("refs/heads/main")},
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/heads/main",
			wantErr:  nil,
		},
		{
			name:     "with exact ref - mismatch",
			wantRepo: "https://github.com/myorg/myrepo",
			opts:     []SourceOption{WithRef("refs/heads/main")},
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/heads/develop",
			wantErr:  ErrRefMismatch,
		},
		{
			name:     "with branches - matches",
			wantRepo: "https://github.com/myorg/myrepo",
			opts:     []SourceOption{WithBranches("main", "release/*")},
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/heads/release/v1",
			wantErr:  nil,
		},
		{
			name:     "with branches - no match",
			wantRepo: "https://github.com/myorg/myrepo",
			opts:     []SourceOption{WithBranches("main")},
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/heads/develop",
			wantErr:  ErrRefMismatch,
		},
		{
			name:     "with tags - matches",
			wantRepo: "https://github.com/myorg/myrepo",
			opts:     []SourceOption{WithTags("v*")},
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/tags/v1.0.0",
			wantErr:  nil,
		},
		{
			name:     "with tags - no match",
			wantRepo: "https://github.com/myorg/myrepo",
			opts:     []SourceOption{WithTags("v*")},
			gotRepo:  "https://github.com/myorg/myrepo",
			gotRef:   "refs/heads/main",
			wantErr:  ErrRefMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := RequireSource(tt.wantRepo, tt.opts...)
			require.NotNil(t, policy)

			attDigest := digest.FromString("attestation")
			manifestDigest := digest.FromString("manifest")

			statement := createSLSAv1Statement("https://github.com/slsa-framework/slsa-github-generator", tt.gotRepo, tt.gotRef, ".github/workflows/build.yml")
			envelope := createDSSEEnvelope(statement)

			mockClient := &mockPolicyClient{
				referrers: []ocispec.Descriptor{
					{
						MediaType:    InTotoArtifactType,
						Digest:       attDigest,
						Size:         int64(len(envelope)),
						ArtifactType: InTotoArtifactType,
					},
				},
				descriptors: map[string][]byte{
					attDigest.String(): envelope,
				},
			}

			req := registry.PolicyRequest{
				Ref:    "example.com/repo:tag",
				Digest: manifestDigest.String(),
				Subject: ocispec.Descriptor{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    manifestDigest,
					Size:      100,
				},
				Client: mockClient,
			}

			err := policy.Evaluate(context.Background(), req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGitHubActionsWorkflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		repo         string
		opts         []any
		gotRepo      string
		gotRef       string
		workflowPath string
		wantErr      error
	}{
		{
			name:         "matches repo",
			repo:         "myorg/myrepo",
			gotRepo:      "https://github.com/myorg/myrepo",
			gotRef:       "refs/heads/main",
			workflowPath: ".github/workflows/build.yml",
			wantErr:      nil,
		},
		{
			name:         "repo mismatch",
			repo:         "myorg/myrepo",
			gotRepo:      "https://github.com/other/repo",
			gotRef:       "refs/heads/main",
			workflowPath: ".github/workflows/build.yml",
			wantErr:      ErrSourceMismatch,
		},
		{
			name:         "with workflow path - matches",
			repo:         "myorg/myrepo",
			opts:         []any{WithWorkflowPath(".github/workflows/release.yml")},
			gotRepo:      "https://github.com/myorg/myrepo",
			gotRef:       "refs/heads/main",
			workflowPath: ".github/workflows/release.yml",
			wantErr:      nil,
		},
		{
			name:         "with workflow path - mismatch",
			repo:         "myorg/myrepo",
			opts:         []any{WithWorkflowPath(".github/workflows/release.yml")},
			gotRepo:      "https://github.com/myorg/myrepo",
			gotRef:       "refs/heads/main",
			workflowPath: ".github/workflows/ci.yml",
			wantErr:      ErrWorkflowMismatch,
		},
		{
			name:         "with branches - matches",
			repo:         "myorg/myrepo",
			opts:         []any{WithWorkflowBranches("main", "release/*")},
			gotRepo:      "https://github.com/myorg/myrepo",
			gotRef:       "refs/heads/release/v1",
			workflowPath: ".github/workflows/build.yml",
			wantErr:      nil,
		},
		{
			name:         "with branches - no match",
			repo:         "myorg/myrepo",
			opts:         []any{WithWorkflowBranches("main")},
			gotRepo:      "https://github.com/myorg/myrepo",
			gotRef:       "refs/heads/develop",
			workflowPath: ".github/workflows/build.yml",
			wantErr:      ErrRefMismatch,
		},
		{
			name:         "with tags - matches",
			repo:         "myorg/myrepo",
			opts:         []any{WithWorkflowTags("v*")},
			gotRepo:      "https://github.com/myorg/myrepo",
			gotRef:       "refs/tags/v1.0.0",
			workflowPath: ".github/workflows/build.yml",
			wantErr:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy, err := GitHubActionsWorkflow(tt.repo, tt.opts...)
			require.NoError(t, err)

			attDigest := digest.FromString("attestation")
			manifestDigest := digest.FromString("manifest")

			statement := createSLSAv1Statement("https://github.com/slsa-framework/slsa-github-generator", tt.gotRepo, tt.gotRef, tt.workflowPath)
			envelope := createDSSEEnvelope(statement)

			mockClient := &mockPolicyClient{
				referrers: []ocispec.Descriptor{
					{
						MediaType:    InTotoArtifactType,
						Digest:       attDigest,
						Size:         int64(len(envelope)),
						ArtifactType: InTotoArtifactType,
					},
				},
				descriptors: map[string][]byte{
					attDigest.String(): envelope,
				},
			}

			req := registry.PolicyRequest{
				Ref:    "example.com/repo:tag",
				Digest: manifestDigest.String(),
				Subject: ocispec.Descriptor{
					MediaType: "application/vnd.oci.image.manifest.v1+json",
					Digest:    manifestDigest,
					Size:      100,
				},
				Client: mockClient,
			}

			err = policy.Evaluate(context.Background(), req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGitHubActionsWorkflow_EmptyRepo(t *testing.T) {
	t.Parallel()

	_, err := GitHubActionsWorkflow("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestPolicy_NoAttestations(t *testing.T) {
	t.Parallel()

	policy := RequireBuilder("https://example.com/builder")
	require.NotNil(t, policy)

	manifestDigest := digest.FromString("manifest")

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{},
	}

	req := registry.PolicyRequest{
		Ref:    "example.com/repo:tag",
		Digest: manifestDigest.String(),
		Subject: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    manifestDigest,
			Size:      100,
		},
		Client: mockClient,
	}

	err := policy.Evaluate(context.Background(), req)
	require.ErrorIs(t, err, ErrNoAttestations)
}

func TestPolicy_ReferrersUnsupported(t *testing.T) {
	t.Parallel()

	policy := RequireBuilder("https://example.com/builder")
	require.NotNil(t, policy)

	manifestDigest := digest.FromString("manifest")

	mockClient := &mockPolicyClient{
		referrerErr: registry.ErrReferrersUnsupported,
	}

	req := registry.PolicyRequest{
		Ref:    "example.com/repo:tag",
		Digest: manifestDigest.String(),
		Subject: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    manifestDigest,
			Size:      100,
		},
		Client: mockClient,
	}

	err := policy.Evaluate(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support referrers")
}

func TestParseProvenance_ValidDSSE(t *testing.T) {
	t.Parallel()

	statement := createSLSAv1Statement(
		"https://github.com/slsa-framework/slsa-github-generator",
		"https://github.com/myorg/myrepo",
		"refs/heads/main",
		".github/workflows/build.yml",
	)
	envelope := createDSSEEnvelope(statement)

	prov, err := ParseProvenance(envelope)
	require.NoError(t, err)
	assert.Equal(t, "https://slsa.dev/provenance/v1", prov.PredicateType)
	assert.Equal(t, "https://github.com/slsa-framework/slsa-github-generator", prov.BuilderID)
	assert.Equal(t, "https://github.com/myorg/myrepo", prov.SourceRepo)
	assert.Equal(t, "refs/heads/main", prov.SourceRef)
	assert.Equal(t, ".github/workflows/build.yml", prov.WorkflowPath)
}

func TestParseProvenance_SigstoreBundle(t *testing.T) {
	t.Parallel()

	statement := createSLSAv1Statement(
		"https://github.com/slsa-framework/slsa-github-generator",
		"https://github.com/myorg/myrepo",
		"refs/tags/v1.0.0",
		".github/workflows/release.yml",
	)
	payload, _ := json.Marshal(statement)

	bundle := map[string]any{
		"mediaType": SigstoreBundleArtifactType,
		"dsseEnvelope": map[string]any{
			"payloadType": "application/vnd.in-toto+json",
			"payload":     base64.StdEncoding.EncodeToString(payload),
			"signatures":  []any{},
		},
	}
	data, _ := json.Marshal(bundle)

	prov, err := ParseProvenance(data)
	require.NoError(t, err)
	assert.Equal(t, "https://slsa.dev/provenance/v1", prov.PredicateType)
	assert.Equal(t, "https://github.com/slsa-framework/slsa-github-generator", prov.BuilderID)
}

func TestParseProvenance_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseProvenance([]byte("not json"))
	require.ErrorIs(t, err, ErrInvalidProvenance)
}

func TestParseProvenance_WrongPayloadType(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"payloadType": "application/octet-stream",
		"payload":     base64.StdEncoding.EncodeToString([]byte("{}")),
		"signatures":  []any{},
	}
	data, _ := json.Marshal(envelope)

	_, err := ParseProvenance(data)
	require.ErrorIs(t, err, ErrInvalidProvenance)
	assert.Contains(t, err.Error(), "unexpected payload type")
}

func TestGlobToRegex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		input   string
		match   bool
	}{
		{"v*", "v1.0.0", true},
		{"v*", "v", true},
		{"v*", "1.0.0", false},
		{"release/*", "release/v1", true},
		{"release/*", "release/beta/v1", false},
		{"main", "main", true},
		{"main", "main2", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.input, func(t *testing.T) {
			t.Parallel()
			regexPattern := "^" + globToRegex(tt.pattern) + "$"
			re := regexp.MustCompile(regexPattern)
			assert.Equal(t, tt.match, re.MatchString(tt.input))
		})
	}
}
