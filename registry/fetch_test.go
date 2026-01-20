package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/registry/cache"
)

// mockOCIClient is a minimal test mock for OCIClient that implements only
// the methods needed for Fetch tests. Methods can be configured via function
// fields or will return errNotImplemented by default.
type mockOCIClient struct {
	ResolveFunc       func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error)
	FetchManifestFunc func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error)
	PushBlobFunc      func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error
	PushManifestFunc  func(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error)
	TagFunc           func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error
}

func (m *mockOCIClient) Resolve(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
	if m.ResolveFunc != nil {
		return m.ResolveFunc(ctx, repoRef, ref)
	}
	return ocispec.Descriptor{}, errors.New("Resolve not implemented")
}

func (m *mockOCIClient) FetchManifest(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
	if m.FetchManifestFunc != nil {
		return m.FetchManifestFunc(ctx, repoRef, expected)
	}
	return ocispec.Manifest{}, nil, errors.New("FetchManifest not implemented")
}

// Unused methods - implement to satisfy interface.
// These return errors since they shouldn't be called in Fetch tests.
var errNotImplemented = errors.New("not implemented in mock")

func (m *mockOCIClient) PushBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
	if m.PushBlobFunc != nil {
		return m.PushBlobFunc(ctx, repoRef, desc, r)
	}
	return errNotImplemented
}

func (m *mockOCIClient) FetchBlob(context.Context, string, *ocispec.Descriptor) (io.ReadCloser, error) {
	return nil, errNotImplemented
}

func (m *mockOCIClient) PushManifest(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
	if m.PushManifestFunc != nil {
		return m.PushManifestFunc(ctx, repoRef, tag, manifest)
	}
	return ocispec.Descriptor{}, errNotImplemented
}

func (m *mockOCIClient) Tag(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
	if m.TagFunc != nil {
		return m.TagFunc(ctx, repoRef, desc, tag)
	}
	return errNotImplemented
}

func (m *mockOCIClient) BlobURL(string, string) (string, error) {
	return "", errNotImplemented
}

func (m *mockOCIClient) AuthHeaders(context.Context, string) (http.Header, error) {
	return nil, errNotImplemented
}

func (m *mockOCIClient) InvalidateAuthHeaders(string) error {
	return errNotImplemented
}

func (m *mockOCIClient) PushManifestByDigest(context.Context, string, *ocispec.Manifest) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errNotImplemented
}

// testManifest creates a valid blob archive manifest for testing with
// standard index and data layers, config, and created annotation.
func testManifest() ocispec.Manifest {
	return ocispec.Manifest{
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
		Annotations: map[string]string{
			ocispec.AnnotationCreated: "2024-01-15T10:00:00Z",
		},
	}
}

//nolint:gocritic // hugeParam: test helper
func mustMarshalManifest(t *testing.T, manifest ocispec.Manifest) []byte {
	t.Helper()

	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	return raw
}

// memRefCache is a simple in-memory RefCache for testing that stores
// reference-to-digest mappings in a map without any eviction policy.
type memRefCache struct {
	data map[string]string
}

func newMemRefCache() *memRefCache {
	return &memRefCache{data: make(map[string]string)}
}

func (c *memRefCache) GetDigest(ref string) (string, bool) {
	d, ok := c.data[ref]
	return d, ok
}

func (c *memRefCache) PutDigest(ref, dgst string) error {
	c.data[ref] = dgst
	return nil
}

func (c *memRefCache) Delete(ref string) error {
	delete(c.data, ref)
	return nil
}

func (c *memRefCache) MaxBytes() int64            { return 0 }
func (c *memRefCache) SizeBytes() int64           { return 0 }
func (c *memRefCache) Prune(int64) (int64, error) { return 0, nil }

// memManifestCache is a simple in-memory ManifestCache for testing that
// stores raw manifest bytes keyed by digest without any eviction policy.
type memManifestCache struct {
	data map[string][]byte
}

func newMemManifestCache() *memManifestCache {
	return &memManifestCache{data: make(map[string][]byte)}
}

func (c *memManifestCache) GetManifest(dgst string) (*ocispec.Manifest, bool) {
	raw, ok := c.data[dgst]
	if !ok {
		return nil, false
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, false
	}
	return &manifest, true
}

func (c *memManifestCache) PutManifest(dgst string, raw []byte) error {
	c.data[dgst] = append([]byte(nil), raw...)
	return nil
}

func (c *memManifestCache) Delete(dgst string) error {
	delete(c.data, dgst)
	return nil
}

func (c *memManifestCache) MaxBytes() int64            { return 0 }
func (c *memManifestCache) SizeBytes() int64           { return 0 }
func (c *memManifestCache) Prune(int64) (int64, error) { return 0, nil }

// memIndexCache is a simple in-memory IndexCache for testing that
// stores index bytes keyed by digest without any eviction policy.
type memIndexCache struct {
	data map[string][]byte
}

func newMemIndexCache() *memIndexCache {
	return &memIndexCache{data: make(map[string][]byte)}
}

func (c *memIndexCache) GetIndex(dgst string) ([]byte, bool) {
	raw, ok := c.data[dgst]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), raw...), true
}

func (c *memIndexCache) PutIndex(dgst string, raw []byte) error {
	c.data[dgst] = append([]byte(nil), raw...)
	return nil
}

func (c *memIndexCache) Delete(dgst string) error {
	delete(c.data, dgst)
	return nil
}

func (c *memIndexCache) MaxBytes() int64            { return 0 }
func (c *memIndexCache) SizeBytes() int64           { return 0 }
func (c *memIndexCache) Prune(int64) (int64, error) { return 0, nil }

func TestClient_Fetch(t *testing.T) {
	t.Parallel()

	const testRef = "registry.example.com/repo:v1.0.0"

	manifest := testManifest()
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
		opts          []FetchOption
		refCache      cache.RefCache
		manifestCache cache.ManifestCache
		setupMock     func(*mockOCIClient)
		wantErr       error
		wantDigest    string
		wantCreated   time.Time
		wantIndexSize int64
		wantDataSize  int64
	}{
		{
			name: "fetch with tag resolves and fetches manifest",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
			},
			wantDigest:    testDigest,
			wantCreated:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			wantIndexSize: 100,
			wantDataSize:  1000,
		},
		{
			name: "fetch with digest skips resolve",
			ref:  "registry.example.com/repo@" + testDigest,
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					t.Error("Resolve should not be called for digest reference")
					return ocispec.Descriptor{}, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: 100,
			wantDataSize:  1000,
		},
		{
			name:     "ref cache hit skips resolve",
			ref:      testRef,
			refCache: &memRefCache{data: map[string]string{testRef: testDigest}},
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					t.Error("Resolve should not be called when ref is cached")
					return ocispec.Descriptor{}, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: 100,
			wantDataSize:  1000,
		},
		{
			name: "manifest cache hit after resolve skips fetch",
			ref:  testRef,
			manifestCache: func() cache.ManifestCache {
				c := newMemManifestCache()
				require.NoError(t, c.PutManifest(testDigest, manifestBytes))
				return c
			}(),
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					t.Error("FetchManifest should not be called when manifest is cached")
					return ocispec.Manifest{}, nil, nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: 100,
			wantDataSize:  1000,
		},
		{
			name:     "both caches hit skips all network calls",
			ref:      testRef,
			refCache: &memRefCache{data: map[string]string{testRef: testDigest}},
			manifestCache: func() cache.ManifestCache {
				c := newMemManifestCache()
				require.NoError(t, c.PutManifest(testDigest, manifestBytes))
				return c
			}(),
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					t.Error("Resolve should not be called when ref is cached")
					return ocispec.Descriptor{}, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					t.Error("FetchManifest should not be called when manifest is cached")
					return ocispec.Manifest{}, nil, nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: 100,
			wantDataSize:  1000,
		},
		{
			name:     "skip cache option bypasses caches",
			ref:      testRef,
			opts:     []FetchOption{WithSkipCache()},
			refCache: &memRefCache{data: map[string]string{testRef: testDigest}},
			manifestCache: func() cache.ManifestCache {
				c := newMemManifestCache()
				require.NoError(t, c.PutManifest(testDigest, manifestBytes))
				return c
			}(),
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					return manifest, manifestBytes, nil
				}
			},
			wantDigest:    testDigest,
			wantIndexSize: 100, // from fresh fetch, not cache
			wantDataSize:  1000,
		},
		{
			name:    "invalid reference returns error",
			ref:     "not a valid ref!!!",
			wantErr: ErrInvalidReference,
		},
		{
			name: "resolve error propagates",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return ocispec.Descriptor{}, errors.New("network error")
				}
			},
			wantErr: errors.New("network error"),
		},
		{
			name: "fetch manifest error propagates",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
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
			name: "invalid artifact type returns error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					invalidManifest := testManifest()
					invalidManifest.ArtifactType = "application/vnd.example.wrong"
					raw := mustMarshalManifest(t, invalidManifest)
					return invalidManifest, raw, nil
				}
			},
			wantErr: ErrInvalidManifest,
		},
		{
			name: "missing index layer returns error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					invalidManifest := ocispec.Manifest{
						MediaType:    ocispec.MediaTypeImageManifest,
						ArtifactType: ArtifactType,
						Layers: []ocispec.Descriptor{
							{MediaType: MediaTypeData, Digest: "sha256:data", Size: 1000},
						},
					}
					raw := mustMarshalManifest(t, invalidManifest)
					return invalidManifest, raw, nil
				}
			},
			wantErr: ErrMissingIndex,
		},
		{
			name: "missing data layer returns error",
			ref:  testRef,
			setupMock: func(m *mockOCIClient) {
				m.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
					return testDesc, nil
				}
				m.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
					invalidManifest := ocispec.Manifest{
						MediaType:    ocispec.MediaTypeImageManifest,
						ArtifactType: ArtifactType,
						Layers: []ocispec.Descriptor{
							{MediaType: MediaTypeIndex, Digest: "sha256:index", Size: 100},
						},
					}
					raw := mustMarshalManifest(t, invalidManifest)
					return invalidManifest, raw, nil
				}
			},
			wantErr: ErrMissingData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockOCIClient{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			c := &Client{
				oci:           mock,
				refCache:      tt.refCache,
				manifestCache: tt.manifestCache,
			}

			manifest, err := c.Fetch(context.Background(), tt.ref, tt.opts...)

			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, ErrInvalidReference) ||
					errors.Is(tt.wantErr, ErrInvalidManifest) ||
					errors.Is(tt.wantErr, ErrMissingIndex) ||
					errors.Is(tt.wantErr, ErrMissingData) {
					assert.ErrorIs(t, err, tt.wantErr)
				} else {
					assert.Contains(t, err.Error(), tt.wantErr.Error())
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, manifest)

			assert.Equal(t, tt.wantDigest, manifest.Digest())
			assert.Equal(t, tt.wantIndexSize, manifest.IndexDescriptor().Size)
			assert.Equal(t, tt.wantDataSize, manifest.DataDescriptor().Size)

			if !tt.wantCreated.IsZero() {
				assert.Equal(t, tt.wantCreated, manifest.Created())
			}
		})
	}
}

func TestClient_Fetch_PopulatesCaches(t *testing.T) {
	t.Parallel()

	const testRef = "registry.example.com/repo:v1.0.0"

	manifest := testManifest()
	manifestBytes := mustMarshalManifest(t, manifest)
	testDigest := digest.FromBytes(manifestBytes).String()

	testDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(testDigest),
		Size:      int64(len(manifestBytes)),
	}

	mock := &mockOCIClient{
		ResolveFunc: func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		},
		FetchManifestFunc: func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error) {
			return manifest, manifestBytes, nil
		},
	}

	refCache := newMemRefCache()
	manifestCache := newMemManifestCache()

	c := &Client{
		oci:           mock,
		refCache:      refCache,
		manifestCache: manifestCache,
	}

	_, err := c.Fetch(context.Background(), testRef)
	require.NoError(t, err)

	// Verify ref cache was populated
	cachedDigest, ok := refCache.GetDigest(testRef)
	assert.True(t, ok, "ref cache should be populated")
	assert.Equal(t, testDigest, cachedDigest)

	// Verify manifest cache was populated
	cachedManifest, ok := manifestCache.GetManifest(testDigest)
	assert.True(t, ok, "manifest cache should be populated")
	assert.Equal(t, ArtifactType, cachedManifest.ArtifactType)
}
