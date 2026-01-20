package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSigner is a test implementation of ManifestSigner.
type mockSigner struct {
	SignFunc func(ctx context.Context, payload []byte) ([]byte, string, error)
}

func (m *mockSigner) SignManifest(ctx context.Context, payload []byte) (data []byte, mediaType string, err error) {
	if m.SignFunc != nil {
		return m.SignFunc(ctx, payload)
	}
	return nil, "", errors.New("SignManifest not implemented")
}

// signMockOCIClient extends mockOCIClient with Sign-specific methods.
type signMockOCIClient struct {
	ResolveFunc              func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error)
	FetchManifestFunc        func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error)
	PushBlobFunc             func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error
	PushManifestByDigestFunc func(ctx context.Context, repoRef string, manifest *ocispec.Manifest) (ocispec.Descriptor, error)
}

func (m *signMockOCIClient) Resolve(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
	if m.ResolveFunc != nil {
		return m.ResolveFunc(ctx, repoRef, ref)
	}
	return ocispec.Descriptor{}, errors.New("Resolve not implemented")
}

func (m *signMockOCIClient) FetchManifest(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
	if m.FetchManifestFunc != nil {
		return m.FetchManifestFunc(ctx, repoRef, expected)
	}
	return ocispec.Manifest{}, nil, errors.New("FetchManifest not implemented")
}

func (m *signMockOCIClient) PushBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
	if m.PushBlobFunc != nil {
		return m.PushBlobFunc(ctx, repoRef, desc, r)
	}
	return errors.New("PushBlob not implemented")
}

func (m *signMockOCIClient) PushManifestByDigest(ctx context.Context, repoRef string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
	if m.PushManifestByDigestFunc != nil {
		return m.PushManifestByDigestFunc(ctx, repoRef, manifest)
	}
	return ocispec.Descriptor{}, errors.New("PushManifestByDigest not implemented")
}

// Unused methods - implement to satisfy OCIClient interface.
func (m *signMockOCIClient) FetchBlob(context.Context, string, *ocispec.Descriptor) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (m *signMockOCIClient) PushManifest(context.Context, string, string, *ocispec.Manifest) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errors.New("not implemented")
}

func (m *signMockOCIClient) Tag(context.Context, string, *ocispec.Descriptor, string) error {
	return errors.New("not implemented")
}

func (m *signMockOCIClient) BlobURL(string, string) (string, error) {
	return "", errors.New("not implemented")
}

func (m *signMockOCIClient) AuthHeaders(context.Context, string) (http.Header, error) {
	return nil, errors.New("not implemented")
}

func (m *signMockOCIClient) InvalidateAuthHeaders(string) error {
	return errors.New("not implemented")
}

func TestSign(t *testing.T) {
	t.Parallel()

	// Create a test manifest
	manifest := ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: ArtifactType,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeEmptyJSON,
			Digest:    digest.FromString("config"),
			Size:      2,
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: MediaTypeIndex,
				Digest:    digest.FromString("index"),
				Size:      100,
			},
			{
				MediaType: MediaTypeData,
				Digest:    digest.FromString("data"),
				Size:      1000,
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestJSON)

	testCases := []struct {
		name      string
		ref       string
		setupMock func(*signMockOCIClient, *mockSigner)
		wantErr   string
	}{
		{
			name: "successful sign",
			ref:  "example.com/repo:v1",
			setupMock: func(m *signMockOCIClient, s *mockSigner) {
				m.ResolveFunc = func(_ context.Context, _, _ string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    manifestDigest,
						Size:      int64(len(manifestJSON)),
					}, nil
				}
				m.FetchManifestFunc = func(_ context.Context, _ string, _ *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestJSON, nil
				}
				m.PushBlobFunc = func(_ context.Context, _ string, _ *ocispec.Descriptor, _ io.Reader) error {
					return nil
				}
				m.PushManifestByDigestFunc = func(_ context.Context, _ string, m *ocispec.Manifest) (ocispec.Descriptor, error) {
					// Verify the referrer manifest structure
					assert.NotNil(t, m.Subject)
					assert.Equal(t, manifestDigest, m.Subject.Digest)
					assert.Equal(t, "application/vnd.test.signature+json", m.ArtifactType)
					assert.Len(t, m.Layers, 1)

					sigManifestJSON, _ := json.Marshal(m)
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    digest.FromBytes(sigManifestJSON),
						Size:      int64(len(sigManifestJSON)),
					}, nil
				}
				s.SignFunc = func(_ context.Context, payload []byte) ([]byte, string, error) {
					// Verify payload is the manifest
					assert.Equal(t, manifestJSON, payload)
					return []byte(`{"signature": "test"}`), "application/vnd.test.signature+json", nil
				}
			},
		},
		{
			name:    "missing reference",
			ref:     "example.com/repo",
			wantErr: "reference must include a tag or digest",
			setupMock: func(_ *signMockOCIClient, _ *mockSigner) {
				// No setup needed - should fail on validation
			},
		},
		{
			name: "resolve error",
			ref:  "example.com/repo:v1",
			setupMock: func(m *signMockOCIClient, _ *mockSigner) {
				m.ResolveFunc = func(_ context.Context, _, _ string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{}, errors.New("resolve failed")
				}
			},
			wantErr: "resolve manifest",
		},
		{
			name: "fetch manifest error",
			ref:  "example.com/repo:v1",
			setupMock: func(m *signMockOCIClient, _ *mockSigner) {
				m.ResolveFunc = func(_ context.Context, _, _ string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    manifestDigest,
						Size:      int64(len(manifestJSON)),
					}, nil
				}
				m.FetchManifestFunc = func(_ context.Context, _ string, _ *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return ocispec.Manifest{}, nil, errors.New("fetch failed")
				}
			},
			wantErr: "fetch manifest",
		},
		{
			name: "sign error",
			ref:  "example.com/repo:v1",
			setupMock: func(m *signMockOCIClient, s *mockSigner) {
				m.ResolveFunc = func(_ context.Context, _, _ string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    manifestDigest,
						Size:      int64(len(manifestJSON)),
					}, nil
				}
				m.FetchManifestFunc = func(_ context.Context, _ string, _ *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestJSON, nil
				}
				s.SignFunc = func(_ context.Context, _ []byte) ([]byte, string, error) {
					return nil, "", errors.New("signing failed")
				}
			},
			wantErr: "sign manifest",
		},
		{
			name: "push signature blob error",
			ref:  "example.com/repo:v1",
			setupMock: func(m *signMockOCIClient, s *mockSigner) {
				m.ResolveFunc = func(_ context.Context, _, _ string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    manifestDigest,
						Size:      int64(len(manifestJSON)),
					}, nil
				}
				m.FetchManifestFunc = func(_ context.Context, _ string, _ *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestJSON, nil
				}
				s.SignFunc = func(_ context.Context, _ []byte) ([]byte, string, error) {
					return []byte(`{"signature": "test"}`), "application/vnd.test.signature+json", nil
				}
				pushCount := 0
				m.PushBlobFunc = func(_ context.Context, _ string, _ *ocispec.Descriptor, _ io.Reader) error {
					pushCount++
					if pushCount == 1 {
						return errors.New("push blob failed")
					}
					return nil
				}
			},
			wantErr: "push signature blob",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mock := &signMockOCIClient{}
			signer := &mockSigner{}
			tc.setupMock(mock, signer)

			client := &Client{oci: mock}
			sigDigest, err := client.Sign(context.Background(), tc.ref, signer)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, sigDigest)
		})
	}
}

func TestSignWithDigestRef(t *testing.T) {
	t.Parallel()

	// Create a test manifest
	manifest := ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: ArtifactType,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeEmptyJSON,
			Digest:    digest.FromString("config"),
			Size:      2,
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: MediaTypeIndex,
				Digest:    digest.FromString("index"),
				Size:      100,
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestJSON)

	mock := &signMockOCIClient{
		// Note: Resolve should not be called when ref is already a digest
		FetchManifestFunc: func(_ context.Context, _ string, desc *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
			assert.Equal(t, manifestDigest, desc.Digest)
			return manifest, manifestJSON, nil
		},
		PushBlobFunc: func(_ context.Context, _ string, _ *ocispec.Descriptor, _ io.Reader) error {
			return nil
		},
		PushManifestByDigestFunc: func(_ context.Context, _ string, _ *ocispec.Manifest) (ocispec.Descriptor, error) {
			return ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    digest.FromString("referrer"),
				Size:      100,
			}, nil
		},
	}

	signer := &mockSigner{
		SignFunc: func(_ context.Context, _ []byte) ([]byte, string, error) {
			return []byte(`{"sig": "test"}`), "application/vnd.test+json", nil
		},
	}

	client := &Client{oci: mock}
	ref := "example.com/repo@" + manifestDigest.String()
	result, err := client.Sign(context.Background(), ref, signer)

	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestSignVerifiesReferrerManifestStructure(t *testing.T) {
	t.Parallel()

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeEmptyJSON,
			Digest:    digest.FromString("config"),
			Size:      2,
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestJSON)

	var capturedReferrer *ocispec.Manifest

	mock := &signMockOCIClient{
		ResolveFunc: func(_ context.Context, _, _ string) (ocispec.Descriptor, error) {
			return ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    manifestDigest,
				Size:      int64(len(manifestJSON)),
			}, nil
		},
		FetchManifestFunc: func(_ context.Context, _ string, _ *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
			return manifest, manifestJSON, nil
		},
		PushBlobFunc: func(_ context.Context, _ string, desc *ocispec.Descriptor, r io.Reader) error {
			// Read the blob data to verify it matches what we signed
			blobData, readErr := io.ReadAll(r)
			if readErr != nil {
				return readErr
			}
			assert.Equal(t, int64(len(blobData)), desc.Size)
			return nil
		},
		PushManifestByDigestFunc: func(_ context.Context, _ string, m *ocispec.Manifest) (ocispec.Descriptor, error) {
			capturedReferrer = m
			referrerJSON, _ := json.Marshal(m)
			return ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    digest.FromBytes(referrerJSON),
				Size:      int64(len(referrerJSON)),
			}, nil
		},
	}

	sigData := []byte(`{"bundle": "test-signature-bundle"}`)
	sigMediaType := "application/vnd.dev.sigstore.bundle.v0.3+json"

	signer := &mockSigner{
		SignFunc: func(_ context.Context, payload []byte) ([]byte, string, error) {
			assert.Equal(t, manifestJSON, payload)
			return sigData, sigMediaType, nil
		},
	}

	client := &Client{oci: mock}
	_, err = client.Sign(context.Background(), "example.com/repo:v1", signer)
	require.NoError(t, err)

	// Verify the captured referrer manifest
	require.NotNil(t, capturedReferrer)

	// Check Subject points to original manifest
	require.NotNil(t, capturedReferrer.Subject)
	assert.Equal(t, ocispec.MediaTypeImageManifest, capturedReferrer.Subject.MediaType)
	assert.Equal(t, manifestDigest, capturedReferrer.Subject.Digest)
	assert.Equal(t, int64(len(manifestJSON)), capturedReferrer.Subject.Size)

	// Check ArtifactType is the signature media type
	assert.Equal(t, sigMediaType, capturedReferrer.ArtifactType)

	// Check layers contain the signature blob
	require.Len(t, capturedReferrer.Layers, 1)
	sigLayer := capturedReferrer.Layers[0]
	assert.Equal(t, sigMediaType, sigLayer.MediaType)
	assert.Equal(t, digest.FromBytes(sigData), sigLayer.Digest)
	assert.Equal(t, int64(len(sigData)), sigLayer.Size)

	// Check config is empty JSON
	assert.Equal(t, ocispec.MediaTypeEmptyJSON, capturedReferrer.Config.MediaType)
}
