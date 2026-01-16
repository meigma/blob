package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob"
)

// pullMockOCIClient extends mockOCIClient with Pull-specific methods.
type pullMockOCIClient struct {
	mockOCIClient
	FetchBlobFunc   func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error)
	BlobURLFunc     func(repoRef, digest string) (string, error)
	AuthHeadersFunc func(ctx context.Context, repoRef string) (http.Header, error)
}

func (m *pullMockOCIClient) FetchBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
	if m.FetchBlobFunc != nil {
		return m.FetchBlobFunc(ctx, repoRef, desc)
	}
	return nil, errNotImplemented
}

func (m *pullMockOCIClient) BlobURL(repoRef, dgst string) (string, error) {
	if m.BlobURLFunc != nil {
		return m.BlobURLFunc(repoRef, dgst)
	}
	return "", errNotImplemented
}

func (m *pullMockOCIClient) AuthHeaders(ctx context.Context, repoRef string) (http.Header, error) {
	if m.AuthHeadersFunc != nil {
		return m.AuthHeadersFunc(ctx, repoRef)
	}
	return nil, errNotImplemented
}

func (m *pullMockOCIClient) InvalidateAuthHeaders(string) error {
	return nil
}

// createTestBlobData creates a minimal blob archive and returns the index and data bytes.
func createTestBlobData(t *testing.T) (indexData, dataBytes []byte) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("test content"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	blobFile, err := blob.CreateBlob(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatalf("create test blob: %v", err)
	}
	defer blobFile.Close()

	// Get index data
	indexData = blobFile.IndexData()

	// Get data bytes by streaming
	var buf bytes.Buffer
	_, err = io.Copy(&buf, blobFile.Stream())
	if err != nil {
		t.Fatalf("read data stream: %v", err)
	}

	return indexData, buf.Bytes()
}

// startDataServer starts an HTTP server that serves data with range request support.
func startDataServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle range requests
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			// Parse "bytes=start-end"
			var start, end int64
			_, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
			if err != nil {
				http.Error(w, "invalid range", http.StatusBadRequest)
				return
			}
			if end >= int64(len(data)) {
				end = int64(len(data)) - 1
			}

			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start : end+1])
			return
		}

		// Full content
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))

	t.Cleanup(server.Close)
	return server
}

func TestClient_Pull(t *testing.T) {
	t.Parallel()

	const testRef = "registry.example.com/repo:v1.0.0"
	testDigest := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	testDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(testDigest),
		Size:      500,
	}

	t.Run("successful pull returns lazy blob", func(t *testing.T) {
		t.Parallel()

		indexData, dataBytes := createTestBlobData(t)
		dataServer := startDataServer(t, dataBytes)

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			// Return the index data
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			return dataServer.URL, nil
		}
		mock.AuthHeadersFunc = func(ctx context.Context, repoRef string) (http.Header, error) {
			return http.Header{}, nil
		}

		c := &Client{oci: mock}
		b, err := c.Pull(context.Background(), testRef)

		require.NoError(t, err)
		require.NotNil(t, b)

		// Verify we can read a file from the blob
		content, err := b.ReadFile("test.txt")
		require.NoError(t, err)
		assert.Equal(t, "test content", string(content))
	})

	t.Run("index size limit enforced", func(t *testing.T) {
		t.Parallel()

		indexData, dataBytes := createTestBlobData(t)
		dataServer := startDataServer(t, dataBytes)

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			manifest := testManifest()
			manifest.Layers[0].Size = int64(len(indexData))
			manifest.Layers[1].Size = int64(len(dataBytes))
			return manifest, nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			return dataServer.URL, nil
		}
		mock.AuthHeadersFunc = func(ctx context.Context, repoRef string) (http.Header, error) {
			return http.Header{}, nil
		}

		c := &Client{oci: mock}
		_, err := c.Pull(context.Background(), testRef, WithMaxIndexSize(int64(len(indexData))-1))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "index blob too large")
	})

	t.Run("skip cache option bypasses caches", func(t *testing.T) {
		t.Parallel()

		indexData, dataBytes := createTestBlobData(t)
		dataServer := startDataServer(t, dataBytes)

		resolveCalled := false
		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			resolveCalled = true
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			return dataServer.URL, nil
		}
		mock.AuthHeadersFunc = func(ctx context.Context, repoRef string) (http.Header, error) {
			return http.Header{}, nil
		}

		// Pre-populate caches
		refCache := newMemRefCache()
		refCache.PutDigest(testRef, testDigest)
		manifestCache := newMemManifestCache()
		cachedManifest, _ := parseBlobManifest(&ocispec.Manifest{
			MediaType:    ocispec.MediaTypeImageManifest,
			ArtifactType: ArtifactType,
			Layers: []ocispec.Descriptor{
				{MediaType: MediaTypeIndex, Digest: "sha256:cached", Size: 50},
				{MediaType: MediaTypeData, Digest: "sha256:cached", Size: 500},
			},
		}, testDigest)
		manifestCache.PutManifest(testDigest, cachedManifest)

		c := &Client{
			oci:           mock,
			refCache:      refCache,
			manifestCache: manifestCache,
		}

		_, err := c.Pull(context.Background(), testRef, WithPullSkipCache())

		require.NoError(t, err)
		assert.True(t, resolveCalled, "Resolve should be called when skip cache is set")
	})

	t.Run("blob options are passed through", func(t *testing.T) {
		t.Parallel()

		indexData, dataBytes := createTestBlobData(t)
		dataServer := startDataServer(t, dataBytes)

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			return dataServer.URL, nil
		}
		mock.AuthHeadersFunc = func(ctx context.Context, repoRef string) (http.Header, error) {
			return http.Header{}, nil
		}

		c := &Client{oci: mock}

		// Pass blob options (e.g., WithVerifyOnClose)
		b, err := c.Pull(context.Background(), testRef,
			WithBlobOptions(blob.WithVerifyOnClose(false)))

		require.NoError(t, err)
		require.NotNil(t, b)
	})

	t.Run("fetch error propagates", func(t *testing.T) {
		t.Parallel()

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return ocispec.Descriptor{}, errors.New("resolve failed")
		}

		c := &Client{oci: mock}
		_, err := c.Pull(context.Background(), testRef)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve failed")
	})

	t.Run("fetch index blob error propagates", func(t *testing.T) {
		t.Parallel()

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return nil, errors.New("fetch blob failed")
		}

		c := &Client{oci: mock}
		_, err := c.Pull(context.Background(), testRef)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetch index blob")
	})

	t.Run("blob URL error propagates", func(t *testing.T) {
		t.Parallel()

		indexData, _ := createTestBlobData(t)

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			return "", errors.New("blob URL failed")
		}

		c := &Client{oci: mock}
		_, err := c.Pull(context.Background(), testRef)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "build data blob URL")
	})

	t.Run("auth headers error propagates", func(t *testing.T) {
		t.Parallel()

		indexData, _ := createTestBlobData(t)

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			return "http://example.com/blob", nil
		}
		mock.AuthHeadersFunc = func(ctx context.Context, repoRef string) (http.Header, error) {
			return nil, errors.New("auth headers failed")
		}

		c := &Client{oci: mock}
		_, err := c.Pull(context.Background(), testRef)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "get auth headers")
	})

	t.Run("http source error propagates", func(t *testing.T) {
		t.Parallel()

		indexData, _ := createTestBlobData(t)

		mock := &pullMockOCIClient{}
		mock.ResolveFunc = func(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
			return testDesc, nil
		}
		mock.FetchManifestFunc = func(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
			return testManifest(), nil
		}
		mock.FetchBlobFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(indexData)), nil
		}
		mock.BlobURLFunc = func(repoRef, dgst string) (string, error) {
			// Return an unreachable URL
			return "http://127.0.0.1:1/unreachable", nil
		}
		mock.AuthHeadersFunc = func(ctx context.Context, repoRef string) (http.Header, error) {
			return http.Header{}, nil
		}

		c := &Client{oci: mock}
		_, err := c.Pull(context.Background(), testRef)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create data source")
	})
}

func TestWithBlobOptions(t *testing.T) {
	t.Parallel()

	cfg := pullConfig{}

	opt := blob.WithVerifyOnClose(false)
	WithBlobOptions(opt)(&cfg)

	assert.Len(t, cfg.blobOpts, 1)

	// Additional call appends
	WithBlobOptions(blob.WithMaxFileSize(1000))(&cfg)
	assert.Len(t, cfg.blobOpts, 2)
}

func TestWithPullSkipCache(t *testing.T) {
	t.Parallel()

	cfg := pullConfig{}
	assert.False(t, cfg.skipCache)

	WithPullSkipCache()(&cfg)
	assert.True(t, cfg.skipCache)
}

func TestWithMaxIndexSize(t *testing.T) {
	t.Parallel()

	cfg := pullConfig{maxIndexSize: defaultMaxIndexSize}
	WithMaxIndexSize(123)(&cfg)

	assert.Equal(t, int64(123), cfg.maxIndexSize)
}
