package gittuf

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
func createSLSAv1Statement(sourceRepo, sourceRef, commit string) map[string]any {
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
				"externalParameters": map[string]any{
					"workflow": map[string]any{
						"repository": sourceRepo,
						"ref":        sourceRef,
						"path":       ".github/workflows/build.yml",
					},
				},
				"resolvedDependencies": []any{
					map[string]any{
						"uri": sourceRepo,
						"digest": map[string]any{
							"gitCommit": commit,
						},
					},
				},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{
					"id": "https://github.com/slsa-framework/slsa-github-generator",
				},
			},
		},
	}
}

func TestNewPolicy_NoRepository(t *testing.T) {
	t.Parallel()

	_, err := NewPolicy()
	require.ErrorIs(t, err, ErrNoRepository)
}

func TestNewPolicy_WithRepository(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(WithRepository("https://github.com/test/repo"))
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/test/repo", p.repoURL)
	assert.True(t, p.latestOnly, "latestOnly should be true by default")
}

func TestNewPolicy_WithOptions(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(
		WithRepository("https://github.com/test/repo"),
		WithFullVerification(),
		WithAllowMissingGittuf(),
		WithAllowMissingProvenance(),
		WithOverrideRef("refs/heads/main"),
	)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/test/repo", p.repoURL)
	assert.False(t, p.latestOnly)
	assert.True(t, p.allowMissingGittuf)
	assert.True(t, p.allowMissingProvenance)
	assert.Equal(t, "refs/heads/main", p.overrideRef)
}

func TestGitHubRepository(t *testing.T) {
	t.Parallel()

	p, err := GitHubRepository("myorg", "myrepo")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/myorg/myrepo", p.repoURL)
}

func TestGitHubRepository_WithOptions(t *testing.T) {
	t.Parallel()

	p, err := GitHubRepository("myorg", "myrepo",
		WithAllowMissingGittuf(),
		WithFullVerification(),
	)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/myorg/myrepo", p.repoURL)
	assert.True(t, p.allowMissingGittuf)
	assert.False(t, p.latestOnly)
}

func TestPolicy_NoSLSAProvenance(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(WithRepository("https://github.com/test/repo"))
	require.NoError(t, err)

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

	err = p.Evaluate(context.Background(), req)
	require.ErrorIs(t, err, ErrNoSLSAProvenance)
}

func TestPolicy_AllowMissingProvenance(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(
		WithRepository("https://github.com/test/repo"),
		WithAllowMissingProvenance(),
	)
	require.NoError(t, err)

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

	// Should pass when AllowMissingProvenance is set
	err = p.Evaluate(context.Background(), req)
	require.NoError(t, err)
}

func TestPolicy_ReferrersUnsupported(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(WithRepository("https://github.com/test/repo"))
	require.NoError(t, err)

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

	err = p.Evaluate(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support referrers")
}

func TestPolicy_ExtractSourceInfo(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(WithRepository("https://github.com/test/repo"))
	require.NoError(t, err)

	attDigest := digest.FromString("attestation")
	manifestDigest := digest.FromString("manifest")

	statement := createSLSAv1Statement(
		"https://github.com/myorg/myrepo",
		"refs/heads/main",
		"abc123def456",
	)
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

	info, err := p.extractSourceInfo(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/myorg/myrepo", info.Repo)
	assert.Equal(t, "refs/heads/main", info.Ref)
	assert.Equal(t, "abc123def456", info.Commit)
}

func TestPolicy_ExtractSourceInfo_SigstoreBundle(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(WithRepository("https://github.com/test/repo"))
	require.NoError(t, err)

	statement := createSLSAv1Statement(
		"https://github.com/myorg/myrepo",
		"refs/tags/v1.0.0",
		"deadbeef",
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
	bundleData, _ := json.Marshal(bundle)

	attDigest := digest.FromString("attestation")
	manifestDigest := digest.FromString("manifest")

	mockClient := &mockPolicyClient{
		referrers: []ocispec.Descriptor{
			{
				MediaType:    SigstoreBundleArtifactType,
				Digest:       attDigest,
				Size:         int64(len(bundleData)),
				ArtifactType: SigstoreBundleArtifactType,
			},
		},
		descriptors: map[string][]byte{
			attDigest.String(): bundleData,
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

	info, err := p.extractSourceInfo(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/myorg/myrepo", info.Repo)
	assert.Equal(t, "refs/tags/v1.0.0", info.Ref)
	assert.Equal(t, "deadbeef", info.Commit)
}

// --- Cache tests ---

func TestRepositoryCache_HashURL(t *testing.T) {
	t.Parallel()

	cache := DefaultCache()

	// Same URL should produce same hash
	hash1 := cache.hashURL("https://github.com/test/repo")
	hash2 := cache.hashURL("https://github.com/test/repo")
	assert.Equal(t, hash1, hash2)

	// Different URLs should produce different hashes
	hash3 := cache.hashURL("https://github.com/other/repo")
	assert.NotEqual(t, hash1, hash3)

	// Hash should be 16 characters
	assert.Len(t, hash1, 16)
}

func TestRepositoryCache_Metadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewRepositoryCache(WithCacheBaseDir(tmpDir))

	entryDir := filepath.Join(tmpDir, "testentry")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))

	metaPath := filepath.Join(entryDir, metadataFileName)

	// Write metadata
	cache.updateMetadata(metaPath, "https://github.com/test/repo")

	// Read and verify
	assert.True(t, cache.isValidCache(metaPath))

	// Read the metadata file
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)

	var meta cacheMetadata
	require.NoError(t, json.Unmarshal(data, &meta))
	assert.Equal(t, "https://github.com/test/repo", meta.RepoURL)
	assert.WithinDuration(t, time.Now(), meta.LastUsed, time.Second)
}

func TestRepositoryCache_IsValidCache_Expired(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewRepositoryCache(
		WithCacheBaseDir(tmpDir),
		WithCacheTTLOption(time.Millisecond), // Very short TTL
	)

	entryDir := filepath.Join(tmpDir, "testentry")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))

	metaPath := filepath.Join(entryDir, metadataFileName)

	// Write metadata with old timestamp
	meta := cacheMetadata{
		LastUsed: time.Now().Add(-time.Hour), // 1 hour ago
		RepoURL:  "https://github.com/test/repo",
	}
	data, _ := json.Marshal(meta)
	require.NoError(t, os.WriteFile(metaPath, data, 0o644))

	// Should be expired
	assert.False(t, cache.isValidCache(metaPath))
}

func TestRepositoryCache_IsValidCache_NotExists(t *testing.T) {
	t.Parallel()

	cache := DefaultCache()
	assert.False(t, cache.isValidCache("/nonexistent/path/.metadata"))
}

func TestRepositoryCache_Clear(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewRepositoryCache(WithCacheBaseDir(tmpDir))

	// Create some cache entries
	entry1 := filepath.Join(tmpDir, "entry1")
	entry2 := filepath.Join(tmpDir, "entry2")
	require.NoError(t, os.MkdirAll(entry1, 0o755))
	require.NoError(t, os.MkdirAll(entry2, 0o755))

	// Clear cache
	require.NoError(t, cache.Clear())

	// Directory should not exist
	_, err := os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(err))
}

func TestRepositoryCache_Invalidate(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewRepositoryCache(WithCacheBaseDir(tmpDir))

	repoURL := "https://github.com/test/repo"
	cacheKey := cache.hashURL(repoURL)
	entryDir := filepath.Join(tmpDir, cacheKey)

	// Create cache entry
	require.NoError(t, os.MkdirAll(entryDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(entryDir, "testfile"), []byte("test"), 0o644))

	// Invalidate
	require.NoError(t, cache.Invalidate(repoURL))

	// Entry should not exist
	_, err := os.Stat(entryDir)
	assert.True(t, os.IsNotExist(err))
}

func TestWithCacheTTL(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(
		WithRepository("https://github.com/test/repo"),
		WithCacheTTL(2*time.Hour),
	)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, p.cache.ttl)
}

// Test parseSourceInfo with various formats
func TestParseSourceInfo_InvalidJSON(t *testing.T) {
	t.Parallel()

	p, _ := NewPolicy(WithRepository("https://github.com/test/repo"))
	info := p.parseSourceInfo([]byte("not json"))
	assert.Nil(t, info)
}

func TestParseSourceInfo_WrongPayloadType(t *testing.T) {
	t.Parallel()

	p, _ := NewPolicy(WithRepository("https://github.com/test/repo"))

	envelope := map[string]any{
		"payloadType": "application/octet-stream",
		"payload":     base64.StdEncoding.EncodeToString([]byte("{}")),
		"signatures":  []any{},
	}
	data, _ := json.Marshal(envelope)

	info := p.parseSourceInfo(data)
	assert.Nil(t, info)
}

func TestParseSourceInfo_NonSLSAPredicate(t *testing.T) {
	t.Parallel()

	p, _ := NewPolicy(WithRepository("https://github.com/test/repo"))

	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": "https://example.com/other-predicate",
		"predicate":     map[string]any{},
	}
	envelope := createDSSEEnvelope(statement)

	info := p.parseSourceInfo(envelope)
	assert.Nil(t, info)
}

func TestIsSLSAPredicateType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		predicateType string
		want          bool
	}{
		{"https://slsa.dev/provenance/v1", true},
		{"https://slsa.dev/provenance/v0.2", true},
		{"https://example.com/other", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.predicateType, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isSLSAPredicateType(tt.predicateType))
		})
	}
}

// Verify Policy implements registry.Policy interface
var _ registry.Policy = (*Policy)(nil)
