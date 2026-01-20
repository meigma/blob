package registry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/core/testutil"
)

func TestClient_Inspect(t *testing.T) {
	t.Parallel()

	const testRef = "registry.example.com/repo:v1.0.0"

	// Create a valid index blob for testing
	indexData := testutil.MakeMinimalIndex()
	indexDigest := digest.FromBytes(indexData)

	manifest := testManifest()
	// Update the manifest with correct index digest
	manifest.Layers[0].Digest = indexDigest
	manifest.Layers[0].Size = int64(len(indexData))

	manifestBytes := mustMarshalManifest(t, manifest)
	testDigest := digest.FromBytes(manifestBytes).String()

	testDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(testDigest),
		Size:      int64(len(manifestBytes)),
	}

	tests := []struct {
		name          string
		ref           string
		opts          []InspectOption
		indexCache    *memIndexCache
		setupMock     func(*inspectMockOCIClient)
		wantErr       error
		wantDigest    string
		wantIndexSize int64
		wantDataSize  int64
	}{
		{
			name: "successful inspect fetches manifest and index",
			ref:  testRef,
			setupMock: func(m *inspectMockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
				m.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(indexData)), nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: int64(len(indexData)),
			wantDataSize:  1000,
		},
		{
			name:       "index cache hit skips fetch blob",
			ref:        testRef,
			indexCache: &memIndexCache{data: map[string][]byte{indexDigest.String(): indexData}},
			setupMock: func(m *inspectMockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
				m.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
					t.Error("FetchBlob should not be called when index is cached")
					return nil, errors.New("should not be called")
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: int64(len(indexData)),
			wantDataSize:  1000,
		},
		{
			name:       "skip cache option bypasses index cache",
			ref:        testRef,
			opts:       []InspectOption{WithInspectSkipCache()},
			indexCache: &memIndexCache{data: map[string][]byte{indexDigest.String(): indexData}},
			setupMock: func(m *inspectMockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
				m.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(indexData)), nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: int64(len(indexData)),
			wantDataSize:  1000,
		},
		{
			name:    "invalid reference returns error",
			ref:     "not a valid ref!!!",
			wantErr: ErrInvalidReference,
		},
		{
			name: "fetch manifest error propagates",
			ref:  testRef,
			setupMock: func(m *inspectMockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return ocispec.Manifest{}, nil, errors.New("fetch error")
				}
			},
			wantErr: errors.New("fetch error"),
		},
		{
			name: "fetch index blob error propagates",
			ref:  testRef,
			setupMock: func(m *inspectMockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
				m.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
					return nil, errors.New("blob fetch error")
				}
			},
			wantErr: errors.New("blob fetch error"),
		},
		{
			name: "max index size exceeded returns error",
			ref:  testRef,
			opts: []InspectOption{WithInspectMaxIndexSize(10)}, // Very small limit
			setupMock: func(m *inspectMockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
			},
			wantErr: errors.New("index blob too large"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &inspectMockOCIClient{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			c := &Client{
				oci: mock,
			}
			// Only set indexCache if non-nil to avoid the nil interface vs typed-nil issue
			if tt.indexCache != nil {
				c.indexCache = tt.indexCache
			}

			result, err := c.Inspect(context.Background(), tt.ref, tt.opts...)

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
			require.NotNil(t, result)
			require.NotNil(t, result.Manifest)
			require.NotNil(t, result.IndexData)

			assert.Equal(t, tt.wantDigest, result.Manifest.Digest())
			assert.Equal(t, tt.wantIndexSize, result.Manifest.IndexDescriptor().Size)
			assert.Equal(t, tt.wantDataSize, result.Manifest.DataDescriptor().Size)
			assert.NotEmpty(t, result.IndexData)
		})
	}
}

func TestClient_Inspect_PopulatesIndexCache(t *testing.T) {
	t.Parallel()

	const testRef = "registry.example.com/repo:v1.0.0"

	indexData := testutil.MakeMinimalIndex()
	indexDigest := digest.FromBytes(indexData)

	manifest := testManifest()
	manifest.Layers[0].Digest = indexDigest
	manifest.Layers[0].Size = int64(len(indexData))

	manifestBytes := mustMarshalManifest(t, manifest)
	testDigest := digest.FromBytes(manifestBytes).String()

	testDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(testDigest),
		Size:      int64(len(manifestBytes)),
	}

	mock := &inspectMockOCIClient{
		ResolveFunc: func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		},
		FetchManifestFunc: func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
			return manifest, manifestBytes, nil
		},
		FetchBlobFunc: func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		},
	}

	indexCache := newMemIndexCache()

	c := &Client{
		oci:        mock,
		indexCache: indexCache,
	}

	_, err := c.Inspect(context.Background(), testRef)
	require.NoError(t, err)

	// Verify index cache was populated
	cachedIndex, ok := indexCache.GetIndex(indexDigest.String())
	assert.True(t, ok, "index cache should be populated")
	assert.Equal(t, indexData, cachedIndex)
}

// inspectMockOCIClient is a mock that supports FetchBlob for Inspect tests.
type inspectMockOCIClient struct {
	ResolveFunc       func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error)
	FetchManifestFunc func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error)
	FetchBlobFunc     func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error)
}

func (m *inspectMockOCIClient) Resolve(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
	if m.ResolveFunc != nil {
		return m.ResolveFunc(ctx, repoRef, ref)
	}
	return ocispec.Descriptor{}, errors.New("Resolve not implemented")
}

func (m *inspectMockOCIClient) FetchManifest(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
	if m.FetchManifestFunc != nil {
		return m.FetchManifestFunc(ctx, repoRef, expected)
	}
	return ocispec.Manifest{}, nil, errors.New("FetchManifest not implemented")
}

func (m *inspectMockOCIClient) FetchBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
	if m.FetchBlobFunc != nil {
		return m.FetchBlobFunc(ctx, repoRef, desc)
	}
	return nil, errors.New("FetchBlob not implemented")
}

// Unused methods - implement to satisfy OCIClient interface.
func (m *inspectMockOCIClient) PushBlob(context.Context, string, *ocispec.Descriptor, io.Reader) error {
	return errors.New("not implemented")
}

func (m *inspectMockOCIClient) PushManifest(context.Context, string, string, *ocispec.Manifest) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errors.New("not implemented")
}

func (m *inspectMockOCIClient) Tag(context.Context, string, *ocispec.Descriptor, string) error {
	return errors.New("not implemented")
}

func (m *inspectMockOCIClient) BlobURL(string, string) (string, error) {
	return "", errors.New("not implemented")
}

func (m *inspectMockOCIClient) AuthHeaders(context.Context, string) (http.Header, error) {
	return nil, errors.New("not implemented")
}

func (m *inspectMockOCIClient) InvalidateAuthHeaders(string) error {
	return errors.New("not implemented")
}

func (m *inspectMockOCIClient) PushManifestByDigest(context.Context, string, *ocispec.Manifest) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errors.New("not implemented")
}
