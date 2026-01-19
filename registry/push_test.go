package registry

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	blob "github.com/meigma/blob/core"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestBlob creates a minimal valid blob for testing.
func createTestBlob(t *testing.T) *blob.Blob {
	t.Helper()

	// Create a temporary directory with a test file
	dir := t.TempDir()
	testFile := dir + "/test.txt"
	if err := os.WriteFile(testFile, []byte("test content"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create the blob
	blobFile, err := blob.CreateBlob(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test blob: %v", err)
	}
	t.Cleanup(func() { blobFile.Close() })

	return blobFile.Blob
}

func TestClient_Push(t *testing.T) {
	t.Parallel()

	const testRef = "registry.example.com/repo:v1.0.0"

	tests := []struct {
		name      string
		ref       string
		opts      []PushOption
		setupMock func(*mockOCIClient)
		wantErr   error
	}{
		{
			name: "successful push",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					// Drain the reader to simulate successful push
					_, _ = io.Copy(io.Discard, r)
					return nil
				}
				m.PushManifestFunc = func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    digest.FromString("manifest"),
						Size:      100,
					}, nil
				}
			},
		},
		{
			name: "successful push with additional tags",
			ref:  testRef,
			opts: []PushOption{WithTags("latest", "v1")},
			setupMock: func(m *mockOCIClient) {
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					_, _ = io.Copy(io.Discard, r)
					return nil
				}
				m.PushManifestFunc = func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    digest.FromString("manifest"),
						Size:      100,
					}, nil
				}
				tagCalls := 0
				m.TagFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
					tagCalls++
					switch tagCalls {
					case 1:
						assert.Equal(t, "latest", tag)
					case 2:
						assert.Equal(t, "v1", tag)
					}
					return nil
				}
			},
		},
		{
			name: "successful push with custom annotations",
			ref:  testRef,
			opts: []PushOption{WithAnnotations(map[string]string{
				"org.example.version": "1.0.0",
			})},
			setupMock: func(m *mockOCIClient) {
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					_, _ = io.Copy(io.Discard, r)
					return nil
				}
				m.PushManifestFunc = func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
					// Verify custom annotation is present
					assert.Equal(t, "1.0.0", manifest.Annotations["org.example.version"])
					// Verify created annotation is auto-set
					assert.NotEmpty(t, manifest.Annotations[ocispec.AnnotationCreated])
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    digest.FromString("manifest"),
						Size:      100,
					}, nil
				}
			},
		},
		{
			name:    "invalid reference",
			ref:     "not a valid ref!!!",
			wantErr: ErrInvalidReference,
		},
		{
			name:    "reference without tag (digest only)",
			ref:     "registry.example.com/repo@sha256:abc123",
			wantErr: ErrInvalidReference,
		},
		{
			name: "push config error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				callCount := 0
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					callCount++
					if callCount == 1 {
						// First call is config blob
						return errors.New("config push failed")
					}
					return nil
				}
			},
			wantErr: errors.New("push config"),
		},
		{
			name: "push index blob error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				callCount := 0
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					callCount++
					if callCount == 1 {
						// Config blob succeeds
						_, _ = io.Copy(io.Discard, r)
						return nil
					}
					if callCount == 2 {
						// Index blob fails
						return errors.New("index push failed")
					}
					return nil
				}
			},
			wantErr: errors.New("push index blob"),
		},
		{
			name: "push data blob error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				callCount := 0
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					callCount++
					if callCount <= 2 {
						// Config and index succeed
						_, _ = io.Copy(io.Discard, r)
						return nil
					}
					// Data blob fails
					return errors.New("data push failed")
				}
			},
			wantErr: errors.New("push data blob"),
		},
		{
			name: "push manifest error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					_, _ = io.Copy(io.Discard, r)
					return nil
				}
				m.PushManifestFunc = func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{}, errors.New("manifest push failed")
				}
			},
			wantErr: errors.New("push manifest"),
		},
		{
			name: "tag error",
			ref:  testRef,
			opts: []PushOption{WithTags("latest")},
			setupMock: func(m *mockOCIClient) {
				m.PushBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
					_, _ = io.Copy(io.Discard, r)
					return nil
				}
				m.PushManifestFunc = func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{
						MediaType: ocispec.MediaTypeImageManifest,
						Digest:    digest.FromString("manifest"),
						Size:      100,
					}, nil
				}
				m.TagFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
					return errors.New("tag failed")
				}
			},
			wantErr: errors.New("tag"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test blob
			testBlob := createTestBlob(t)

			mock := &mockOCIClient{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			c := &Client{oci: mock}

			err := c.Push(context.Background(), tt.ref, testBlob, tt.opts...)

			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, ErrInvalidReference) {
					assert.ErrorIs(t, err, ErrInvalidReference)
				} else {
					assert.Contains(t, err.Error(), tt.wantErr.Error())
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_Push_VerifiesManifestStructure(t *testing.T) {
	t.Parallel()

	testBlob := createTestBlob(t)

	var capturedManifest *ocispec.Manifest
	mock := &mockOCIClient{
		PushBlobFunc: func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
			_, _ = io.Copy(io.Discard, r)
			return nil
		},
		PushManifestFunc: func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
			capturedManifest = manifest
			return ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    digest.FromString("manifest"),
				Size:      100,
			}, nil
		},
	}

	c := &Client{oci: mock}
	err := c.Push(context.Background(), "registry.example.com/repo:v1.0.0", testBlob)
	require.NoError(t, err)
	require.NotNil(t, capturedManifest)

	// Verify manifest structure
	assert.Equal(t, 2, capturedManifest.SchemaVersion)
	assert.Equal(t, ocispec.MediaTypeImageManifest, capturedManifest.MediaType)
	assert.Equal(t, ArtifactType, capturedManifest.ArtifactType)

	// Verify config
	assert.Equal(t, ocispec.MediaTypeEmptyJSON, capturedManifest.Config.MediaType)
	assert.NotEmpty(t, capturedManifest.Config.Digest)

	// Verify layers (index and data)
	require.Len(t, capturedManifest.Layers, 2)
	assert.Equal(t, MediaTypeIndex, capturedManifest.Layers[0].MediaType)
	assert.Equal(t, MediaTypeData, capturedManifest.Layers[1].MediaType)

	// Verify annotations
	assert.NotEmpty(t, capturedManifest.Annotations[ocispec.AnnotationCreated])
}

func TestClient_Push_VerifiesBlobDescriptors(t *testing.T) {
	t.Parallel()

	testBlob := createTestBlob(t)

	var blobDescs []ocispec.Descriptor
	mock := &mockOCIClient{
		PushBlobFunc: func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
			blobDescs = append(blobDescs, *desc)
			_, _ = io.Copy(io.Discard, r)
			return nil
		},
		PushManifestFunc: func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
			return ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    digest.FromString("manifest"),
				Size:      100,
			}, nil
		},
	}

	c := &Client{oci: mock}
	err := c.Push(context.Background(), "registry.example.com/repo:v1.0.0", testBlob)
	require.NoError(t, err)

	// Should have 3 blobs: config, index, data
	require.Len(t, blobDescs, 3)

	// Config blob
	assert.Equal(t, ocispec.MediaTypeEmptyJSON, blobDescs[0].MediaType)
	assert.Equal(t, int64(2), blobDescs[0].Size) // "{}" is 2 bytes

	// Index blob
	assert.Equal(t, MediaTypeIndex, blobDescs[1].MediaType)
	assert.Greater(t, blobDescs[1].Size, int64(0))
	assert.NotEmpty(t, blobDescs[1].Digest)

	// Data blob
	assert.Equal(t, MediaTypeData, blobDescs[2].MediaType)
	assert.Greater(t, blobDescs[2].Size, int64(0))
	assert.NotEmpty(t, blobDescs[2].Digest)
}

func TestWithTags(t *testing.T) {
	t.Parallel()

	cfg := pushConfig{}

	WithTags("v1", "latest")(&cfg)
	assert.Equal(t, []string{"v1", "latest"}, cfg.tags)

	// Additional call appends
	WithTags("v2")(&cfg)
	assert.Equal(t, []string{"v1", "latest", "v2"}, cfg.tags)
}

func TestWithAnnotations(t *testing.T) {
	t.Parallel()

	cfg := pushConfig{}

	WithAnnotations(map[string]string{
		"key1": "value1",
	})(&cfg)
	assert.Equal(t, "value1", cfg.annotations["key1"])

	// Additional call merges
	WithAnnotations(map[string]string{
		"key2": "value2",
	})(&cfg)
	assert.Equal(t, "value1", cfg.annotations["key1"])
	assert.Equal(t, "value2", cfg.annotations["key2"])

	// Can override
	WithAnnotations(map[string]string{
		"key1": "newvalue",
	})(&cfg)
	assert.Equal(t, "newvalue", cfg.annotations["key1"])
}
