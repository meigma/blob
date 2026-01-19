package client

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/meigma/blob/core"
	blobhttp "github.com/meigma/blob/core/http"
)

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

	// Step 1: Fetch manifest (handles caching internally)
	var fetchOpts []FetchOption
	if cfg.skipCache {
		fetchOpts = append(fetchOpts, WithSkipCache())
	}
	manifest, err := c.Fetch(ctx, ref, fetchOpts...)
	if err != nil {
		return nil, err
	}

	// Step 2: Fetch index blob (small, download fully)
	indexDesc := manifest.IndexDescriptor()
	indexDigest := indexDesc.Digest.String()
	if cfg.maxIndexSize > 0 && indexDesc.Size > cfg.maxIndexSize {
		return nil, fmt.Errorf("read index blob: %w", fmt.Errorf("index blob too large: %d > %d", indexDesc.Size, cfg.maxIndexSize))
	}

	var (
		indexData []byte
		fromCache bool
	)
	if !cfg.skipCache && c.indexCache != nil {
		if cached, ok := c.indexCache.GetIndex(indexDigest); ok {
			if cfg.maxIndexSize > 0 && int64(len(cached)) > cfg.maxIndexSize {
				return nil, fmt.Errorf("read index blob: %w", fmt.Errorf("index blob too large: %d > %d", len(cached), cfg.maxIndexSize))
			}
			cacheValid := true
			if indexDesc.Size > 0 && int64(len(cached)) != indexDesc.Size {
				cacheValid = false
			}
			if cacheValid {
				if err := indexDesc.Digest.Validate(); err != nil {
					cacheValid = false
				} else if indexDesc.Digest.Algorithm().FromBytes(cached) != indexDesc.Digest {
					cacheValid = false
				}
			}
			if cacheValid {
				indexData = cached
				fromCache = true
			} else {
				_ = c.indexCache.Delete(indexDigest)
			}
		}
	}

	if !fromCache {
		indexReader, err := c.oci.FetchBlob(ctx, ref, &indexDesc)
		if err != nil {
			return nil, fmt.Errorf("fetch index blob: %w", mapOCIError(err))
		}
		defer indexReader.Close()

		indexData, err = readIndexData(indexReader, indexDesc.Size, cfg.maxIndexSize)
		if err != nil {
			return nil, fmt.Errorf("read index blob: %w", err)
		}

		if c.indexCache != nil {
			if err := c.indexCache.PutIndex(indexDigest, indexData); err != nil {
				return nil, fmt.Errorf("cache index: %w", err)
			}
		}
	}

	if err := indexDesc.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("read index blob: %w: invalid digest %q: %v", ErrInvalidManifest, indexDesc.Digest, err)
	}
	if computed := indexDesc.Digest.Algorithm().FromBytes(indexData); computed != indexDesc.Digest {
		return nil, fmt.Errorf("read index blob: %w: expected %s, got %s", ErrDigestMismatch, indexDesc.Digest, computed)
	}

	// Step 3: Build data blob URL for range requests
	dataDesc := manifest.DataDescriptor()
	dataURL, err := c.oci.BlobURL(ref, dataDesc.Digest.String())
	if err != nil {
		return nil, fmt.Errorf("build data blob URL: %w", err)
	}

	// Step 4: Create HTTP source for lazy data access
	var sourceOpts []blobhttp.Option
	if provider, ok := c.oci.(authClientProvider); ok {
		authClient, err := provider.AuthClient(ref)
		if err != nil {
			return nil, fmt.Errorf("get auth client: %w", err)
		}
		sourceOpts = append(sourceOpts, blobhttp.WithClient(authClient))
	} else {
		headers, err := c.oci.AuthHeaders(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("get auth headers: %w", mapOCIError(err))
		}
		sourceOpts = append(sourceOpts, blobhttp.WithHeaders(headers))
	}

	source, err := blobhttp.NewSource(dataURL, sourceOpts...)
	if err != nil {
		return nil, fmt.Errorf("create data source: %w", err)
	}

	// Step 6: Create Blob with index data and lazy data source
	return blob.New(indexData, source, cfg.blobOpts...)
}

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
