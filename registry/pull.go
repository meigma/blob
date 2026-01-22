package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	blob "github.com/meigma/blob/core"
	blobhttp "github.com/meigma/blob/core/http"
)

// authClientProvider is an optional interface that OCIClient implementations
// can provide to supply a pre-configured HTTP client with authentication.
type authClientProvider interface {
	AuthClient(repoRef string) (*http.Client, error)
}

// Pull retrieves a blob archive from an OCI registry.
//
// The returned Blob is lazy: file data is fetched on demand via HTTP range
// requests. The index blob is downloaded immediately as it is small.
//
// The caller should close the Blob when done if it wraps file resources.
func (c *Client) Pull(ctx context.Context, ref string, opts ...PullOption) (*blob.Blob, error) {
	cfg := pullConfig{
		maxIndexSize: defaultMaxIndexSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	c.log().Info("pulling archive", "ref", ref)

	// Step 1: Fetch manifest (handles caching internally)
	reportPullProgress(cfg.progress, blob.StageFetchingManifest, 0, 0)
	var fetchOpts []FetchOption
	if cfg.skipCache {
		fetchOpts = append(fetchOpts, WithSkipCache())
	}
	manifest, err := c.Fetch(ctx, ref, fetchOpts...)
	if err != nil {
		return nil, err
	}
	reportPullProgress(cfg.progress, blob.StageFetchingManifest, 1, 1)

	// Step 2: Fetch index blob (small, download fully)
	indexDesc := manifest.IndexDescriptor()
	reportPullProgress(cfg.progress, blob.StageFetchingIndex, 0, sizeToUint64(indexDesc.Size))
	indexData, err := c.fetchIndexBlob(ctx, ref, manifest, &cfg)
	if err != nil {
		return nil, err
	}
	reportPullProgress(cfg.progress, blob.StageFetchingIndex, uint64(len(indexData)), uint64(len(indexData)))

	// Step 3: Create HTTP source for lazy data access
	source, err := c.createDataSource(ctx, ref, manifest)
	if err != nil {
		return nil, err
	}
	c.log().Debug("created data source", "url", source.SourceID())

	// Step 4: Create Blob with index data and lazy data source
	return blob.New(indexData, source, cfg.blobOpts...)
}

// fetchIndexBlob fetches the index blob, using cache if available.
func (c *Client) fetchIndexBlob(ctx context.Context, ref string, manifest *BlobManifest, cfg *pullConfig) ([]byte, error) {
	indexDesc := manifest.IndexDescriptor()
	indexDigest := indexDesc.Digest.String()

	if cfg.maxIndexSize > 0 && indexDesc.Size > cfg.maxIndexSize {
		return nil, fmt.Errorf("read index blob: index blob too large: %d > %d", indexDesc.Size, cfg.maxIndexSize)
	}

	// Try cache first
	if indexData, ok := c.tryIndexCache(indexDigest, &indexDesc, cfg); ok {
		return indexData, nil
	}

	// Fetch from registry
	indexReader, err := c.oci.FetchBlob(ctx, ref, &indexDesc)
	if err != nil {
		return nil, fmt.Errorf("fetch index blob: %w", mapOCIError(err))
	}
	defer indexReader.Close()

	indexData, err := readIndexData(indexReader, indexDesc.Size, cfg.maxIndexSize)
	if err != nil {
		return nil, fmt.Errorf("read index blob: %w", err)
	}

	// Verify digest
	if err := c.verifyIndexDigest(indexData, &indexDesc); err != nil {
		return nil, err
	}

	// Store in cache
	if c.indexCache != nil {
		if err := c.indexCache.PutIndex(indexDigest, indexData); err != nil {
			return nil, fmt.Errorf("cache index: %w", err)
		}
	}

	return indexData, nil
}

// tryIndexCache attempts to get the index from cache, returning (data, true) on hit.
func (c *Client) tryIndexCache(indexDigest string, indexDesc *ocispec.Descriptor, cfg *pullConfig) ([]byte, bool) {
	if cfg.skipCache || c.indexCache == nil {
		return nil, false
	}

	cached, ok := c.indexCache.GetIndex(indexDigest)
	if !ok {
		c.log().Debug("index cache miss", "digest", indexDigest[:min(16, len(indexDigest))])
		return nil, false
	}

	if cfg.maxIndexSize > 0 && int64(len(cached)) > cfg.maxIndexSize {
		return nil, false
	}

	if !c.validateCachedIndex(cached, indexDesc) {
		c.log().Warn("corrupted index cache entry deleted", "digest", indexDigest[:min(16, len(indexDigest))])
		_ = c.indexCache.Delete(indexDigest) //nolint:errcheck // best-effort cleanup
		return nil, false
	}

	c.log().Debug("index cache hit", "digest", indexDigest[:min(16, len(indexDigest))], "size", len(cached))
	return cached, true
}

// validateCachedIndex checks if cached data matches the expected descriptor.
func (c *Client) validateCachedIndex(cached []byte, indexDesc *ocispec.Descriptor) bool {
	if indexDesc.Size > 0 && int64(len(cached)) != indexDesc.Size {
		return false
	}
	if err := indexDesc.Digest.Validate(); err != nil {
		return false
	}
	return indexDesc.Digest.Algorithm().FromBytes(cached) == indexDesc.Digest
}

// verifyIndexDigest verifies the index data matches its expected digest.
func (c *Client) verifyIndexDigest(indexData []byte, indexDesc *ocispec.Descriptor) error {
	if err := indexDesc.Digest.Validate(); err != nil {
		return fmt.Errorf("read index blob: %w: invalid digest %q: %v", ErrInvalidManifest, indexDesc.Digest, err)
	}
	if computed := indexDesc.Digest.Algorithm().FromBytes(indexData); computed != indexDesc.Digest {
		c.log().Warn("index digest verification failed",
			"expected", indexDesc.Digest.String(),
			"computed", computed.String(),
		)
		return fmt.Errorf("read index blob: %w: expected %s, got %s", ErrDigestMismatch, indexDesc.Digest, computed)
	}
	return nil
}

// createDataSource creates an HTTP source for lazy data blob access.
func (c *Client) createDataSource(ctx context.Context, ref string, manifest *BlobManifest) (*blobhttp.Source, error) {
	dataDesc := manifest.DataDescriptor()
	dataURL, err := c.oci.BlobURL(ref, dataDesc.Digest.String())
	if err != nil {
		return nil, fmt.Errorf("build data blob URL: %w", err)
	}

	var sourceOpts []blobhttp.Option
	if provider, ok := c.oci.(authClientProvider); ok {
		authClient, authErr := provider.AuthClient(ref)
		if authErr != nil {
			return nil, fmt.Errorf("get auth client: %w", authErr)
		}
		sourceOpts = append(sourceOpts, blobhttp.WithClient(authClient))
	} else {
		headers, headerErr := c.oci.AuthHeaders(ctx, ref)
		if headerErr != nil {
			return nil, fmt.Errorf("get auth headers: %w", mapOCIError(headerErr))
		}
		sourceOpts = append(sourceOpts, blobhttp.WithHeaders(headers))
	}

	source, err := blobhttp.NewSource(dataURL, sourceOpts...)
	if err != nil {
		return nil, fmt.Errorf("create data source: %w", err)
	}

	return source, nil
}

// readIndexData reads index data from r with optional size limits.
// If maxSize is positive, reading stops if the limit is exceeded.
func readIndexData(r io.Reader, expectedSize, maxSize int64) ([]byte, error) {
	if maxSize > 0 && expectedSize > maxSize {
		return nil, fmt.Errorf("index blob too large: %d > %d", expectedSize, maxSize)
	}

	reader := r
	if maxSize > 0 {
		reader = io.LimitReader(r, maxSize+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	if maxSize > 0 && int64(len(data)) > maxSize {
		return nil, fmt.Errorf("index blob too large: %d > %d", len(data), maxSize)
	}

	return data, nil
}

// reportPullProgress sends a progress event if a callback is configured.
func reportPullProgress(fn blob.ProgressFunc, stage blob.ProgressStage, bytesDone, bytesTotal uint64) {
	if fn == nil {
		return
	}
	fn(blob.ProgressEvent{
		Stage:      stage,
		BytesDone:  bytesDone,
		BytesTotal: bytesTotal,
	})
}
