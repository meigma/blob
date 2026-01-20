package sigstore

import (
	"context"
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

//nolint:gocritic // signature matches registry.PolicyClient interface
func (m *mockPolicyClient) Referrers(_ context.Context, _ string, _ ocispec.Descriptor, _ string) ([]ocispec.Descriptor, error) {
	if m.referrerErr != nil {
		return nil, m.referrerErr
	}
	return m.referrers, nil
}

//nolint:gocritic // signature matches registry.PolicyClient interface
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

func TestPolicy_NoSignatures(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy()
	if err != nil {
		t.Skipf("skipping test: cannot create policy (network required): %v", err)
	}

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{}, // No signatures
	}

	req := registry.PolicyRequest{
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
	assert.Contains(t, err.Error(), "no signatures found")
}

func TestPolicy_ReferrersUnsupported(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy()
	if err != nil {
		t.Skipf("skipping test: cannot create policy (network required): %v", err)
	}

	mockClient := &mockPolicyClient{
		referrerErr: registry.ErrReferrersUnsupported,
	}

	req := registry.PolicyRequest{
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

func TestPolicy_InvalidBundle(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy()
	if err != nil {
		t.Skipf("skipping test: cannot create policy (network required): %v", err)
	}

	bundleDigest := digest.FromString("invalid bundle")
	manifestDigest := digest.FromString("manifest")

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    SignatureArtifactType,
				Digest:       bundleDigest,
				Size:         100,
				ArtifactType: SignatureArtifactType,
			},
		},
		descriptors: map[string][]byte{
			bundleDigest.String():   []byte("not a valid bundle"),
			manifestDigest.String(): []byte(`{"test":"manifest"}`),
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed")
}

func TestWithIdentity(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(
		WithIdentity("https://accounts.google.com", "user@example.com"),
	)
	if err != nil {
		t.Skipf("skipping test: cannot create policy (network required): %v", err)
	}

	assert.NotNil(t, policy.identity)
}

func TestWithIdentity_InvalidIssuer(t *testing.T) {
	t.Parallel()

	// NewShortCertificateIdentity validates the issuer format
	_, err := NewPolicy(
		WithIdentity("", "user@example.com"),
	)
	// Empty issuer should fail
	require.Error(t, err)
}

func TestWithTrustedRootFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := NewPolicy(
		WithTrustedRootFile("/nonexistent/path/trusted_root.json"),
	)
	require.Error(t, err)
}
