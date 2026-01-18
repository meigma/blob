package opa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/client"
)

// mockPolicyClient implements client.PolicyClient for testing.
type mockPolicyClient struct {
	referrers   []ocispec.Descriptor
	referrerErr error
	descriptors map[string][]byte
	fetchErr    error
}

//nolint:gocritic // implements client.PolicyClient interface
func (m *mockPolicyClient) Referrers(_ context.Context, _ string, _ ocispec.Descriptor, _ string) ([]ocispec.Descriptor, error) {
	if m.referrerErr != nil {
		return nil, m.referrerErr
	}
	return m.referrers, nil
}

//nolint:gocritic // implements client.PolicyClient interface
func (m *mockPolicyClient) FetchDescriptor(_ context.Context, _ string, desc ocispec.Descriptor) ([]byte, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if m.descriptors != nil {
		if data, ok := m.descriptors[desc.Digest.String()]; ok {
			return data, nil
		}
	}
	return nil, client.ErrNotFound
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

// createSLSAStatement creates an in-toto statement with SLSA provenance.
func createSLSAStatement(builderID string) map[string]any {
	return map[string]any{
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
				"resolvedDependencies": []any{
					map[string]any{
						"uri": "git+https://github.com/myorg/myrepo@refs/heads/main",
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
}

func TestNewPolicy_NoPolicy(t *testing.T) {
	t.Parallel()

	_, err := NewPolicy()
	require.ErrorIs(t, err, ErrNoPolicy)
}

func TestNewPolicy_InvalidRegoSyntax(t *testing.T) {
	t.Parallel()

	_, err := NewPolicy(WithPolicy("package blob.policy\n invalid rego syntax {{{"))
	require.Error(t, err)
}

func TestPolicy_NoAttestations(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(WithPolicy(`
		package blob.policy
		default allow := false
	`))
	require.NoError(t, err)

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{},
	}

	req := client.PolicyRequest{
		Ref:    "example.com/repo:tag",
		Digest: "sha256:abc123",
		Subject: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    digest.FromString("test"),
			Size:      100,
		},
		Client: mockClient,
	}

	err = policy.Evaluate(context.Background(), req)
	require.ErrorIs(t, err, ErrNoAttestations)
}

func TestPolicy_ReferrersUnsupported(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(WithPolicy(`
		package blob.policy
		default allow := false
	`))
	require.NoError(t, err)

	mockClient := &mockPolicyClient{
		referrerErr: client.ErrReferrersUnsupported,
	}

	req := client.PolicyRequest{
		Ref:    "example.com/repo:tag",
		Digest: "sha256:abc123",
		Subject: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    digest.FromString("test"),
			Size:      100,
		},
		Client: mockClient,
	}

	err = policy.Evaluate(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support referrers")
}

func TestPolicy_AllowPolicy(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(WithPolicy(`
		package blob.policy
		import rego.v1

		default allow := false

		allow if {
			some att in input.attestations
			att.predicate.runDetails.builder.id == "https://github.com/actions/runner/github-hosted"
		}
	`))
	require.NoError(t, err)

	attDigest := digest.FromString("attestation")
	manifestDigest := digest.FromString("manifest")

	statement := createSLSAStatement("https://github.com/actions/runner/github-hosted")
	envelope := createDSSEEnvelope(statement)

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    DefaultArtifactType,
				Digest:       attDigest,
				Size:         int64(len(envelope)),
				ArtifactType: DefaultArtifactType,
			},
		},
		descriptors: map[string][]byte{
			attDigest.String(): envelope,
		},
	}

	req := client.PolicyRequest{
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
	require.NoError(t, err)
}

func TestPolicy_DenyPolicy(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(WithPolicy(`
		package blob.policy
		import rego.v1

		default allow := false

		allow if {
			some att in input.attestations
			att.predicate.runDetails.builder.id == "https://trusted-builder.example.com"
		}
	`))
	require.NoError(t, err)

	attDigest := digest.FromString("attestation")
	manifestDigest := digest.FromString("manifest")

	// Statement with untrusted builder
	statement := createSLSAStatement("https://untrusted-builder.example.com")
	envelope := createDSSEEnvelope(statement)

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    DefaultArtifactType,
				Digest:       attDigest,
				Size:         int64(len(envelope)),
				ArtifactType: DefaultArtifactType,
			},
		},
		descriptors: map[string][]byte{
			attDigest.String(): envelope,
		},
	}

	req := client.PolicyRequest{
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
	require.ErrorIs(t, err, ErrPolicyDenied)
}

func TestPolicy_DenyWithReasons(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(WithPolicy(`
		package blob.policy
		import rego.v1

		deny contains msg if {
			some att in input.attestations
			att.predicate.runDetails.builder.id != "https://trusted-builder.example.com"
			msg := "untrusted builder"
		}
	`))
	require.NoError(t, err)

	attDigest := digest.FromString("attestation")
	manifestDigest := digest.FromString("manifest")

	statement := createSLSAStatement("https://untrusted-builder.example.com")
	envelope := createDSSEEnvelope(statement)

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    DefaultArtifactType,
				Digest:       attDigest,
				Size:         int64(len(envelope)),
				ArtifactType: DefaultArtifactType,
			},
		},
		descriptors: map[string][]byte{
			attDigest.String(): envelope,
		},
	}

	req := client.PolicyRequest{
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
	require.ErrorIs(t, err, ErrPolicyDenied)
	assert.Contains(t, err.Error(), "untrusted builder")
}

func TestPolicy_InvalidAttestation(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(WithPolicy(`
		package blob.policy
		default allow := true
	`))
	require.NoError(t, err)

	attDigest := digest.FromString("invalid")
	manifestDigest := digest.FromString("manifest")

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    DefaultArtifactType,
				Digest:       attDigest,
				Size:         100,
				ArtifactType: DefaultArtifactType,
			},
		},
		descriptors: map[string][]byte{
			attDigest.String(): []byte("not valid json"),
		},
	}

	req := client.PolicyRequest{
		Ref:    "example.com/repo:tag",
		Digest: manifestDigest.String(),
		Subject: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    manifestDigest,
			Size:      100,
		},
		Client: mockClient,
	}

	// Should fail because all attestations are invalid
	err = policy.Evaluate(context.Background(), req)
	require.ErrorIs(t, err, ErrNoAttestations)
}

func TestPolicy_PredicateTypeFiltering(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(
		WithPolicy(`
			package blob.policy
			import rego.v1

			default allow := false

			allow if {
				count(input.attestations) > 0
			}
		`),
		WithPredicateTypes("https://custom.example.com/attestation/v1"),
	)
	require.NoError(t, err)

	attDigest := digest.FromString("attestation")
	manifestDigest := digest.FromString("manifest")

	// Statement with SLSA predicate type (should be filtered out)
	statement := createSLSAStatement("https://github.com/actions/runner/github-hosted")
	envelope := createDSSEEnvelope(statement)

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    DefaultArtifactType,
				Digest:       attDigest,
				Size:         int64(len(envelope)),
				ArtifactType: DefaultArtifactType,
			},
		},
		descriptors: map[string][]byte{
			attDigest.String(): envelope,
		},
	}

	req := client.PolicyRequest{
		Ref:    "example.com/repo:tag",
		Digest: manifestDigest.String(),
		Subject: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    manifestDigest,
			Size:      100,
		},
		Client: mockClient,
	}

	// Should fail because SLSA predicate is filtered out
	err = policy.Evaluate(context.Background(), req)
	require.ErrorIs(t, err, ErrNoAttestations)
}

func TestWithArtifactType(t *testing.T) {
	t.Parallel()

	customType := "application/vnd.custom.attestation+json"
	policy, err := NewPolicy(
		WithPolicy(`package blob.policy
			default allow := true`),
		WithArtifactType(customType),
	)
	require.NoError(t, err)
	assert.Equal(t, customType, policy.artifactType)
}

func TestParseAttestation_ValidDSSE(t *testing.T) {
	t.Parallel()

	statement := createSLSAStatement("https://github.com/actions/runner/github-hosted")
	envelope := createDSSEEnvelope(statement)

	att, err := parseAttestation(envelope)
	require.NoError(t, err)
	assert.Equal(t, "https://in-toto.io/Statement/v1", att.Type)
	assert.Equal(t, "https://slsa.dev/provenance/v1", att.PredicateType)
	assert.Len(t, att.Subject, 1)
	assert.NotNil(t, att.Predicate)
}

func TestParseAttestation_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseAttestation([]byte("not json"))
	require.ErrorIs(t, err, ErrInvalidAttestation)
}

func TestParseAttestation_WrongPayloadType(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"payloadType": "application/octet-stream",
		"payload":     base64.StdEncoding.EncodeToString([]byte("{}")),
		"signatures":  []any{},
	}
	data, _ := json.Marshal(envelope)

	_, err := parseAttestation(data)
	require.ErrorIs(t, err, ErrInvalidAttestation)
	assert.Contains(t, err.Error(), "unexpected payload type")
}

func TestMatchesPredicateType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		predicateType string
		accepted      []string
		want          bool
	}{
		{
			name:          "empty accepted list matches all",
			predicateType: "https://slsa.dev/provenance/v1",
			accepted:      nil,
			want:          true,
		},
		{
			name:          "matches accepted type",
			predicateType: "https://slsa.dev/provenance/v1",
			accepted:      []string{"https://slsa.dev/provenance/v1"},
			want:          true,
		},
		{
			name:          "no match",
			predicateType: "https://slsa.dev/provenance/v1",
			accepted:      []string{"https://slsa.dev/provenance/v0.2"},
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			att := &AttestationInput{PredicateType: tt.predicateType}
			got := matchesPredicateType(att, tt.accepted)
			assert.Equal(t, tt.want, got)
		})
	}
}
