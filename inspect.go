package blob

import (
	"context"
	"sync"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	blobcore "github.com/meigma/blob/core"
	"github.com/meigma/blob/registry"
)

// InspectResult contains metadata about a blob archive without the data blob.
//
// It provides access to the manifest, file index, and optionally referrers,
// enabling inspection of archive contents without downloading file data.
type InspectResult struct {
	manifest *registry.BlobManifest
	index    *blobcore.IndexView
	client   *Client
	ref      string

	// Lazy computed stats
	statsOnce              sync.Once
	totalUncompressedSize  uint64
	totalCompressedSize    uint64
	compressionRatioResult float64
}

// Manifest returns the OCI manifest metadata.
func (r *InspectResult) Manifest() *registry.BlobManifest {
	return r.manifest
}

// Index returns the file index view.
func (r *InspectResult) Index() *blobcore.IndexView {
	return r.index
}

// Digest returns the manifest digest.
func (r *InspectResult) Digest() string {
	return r.manifest.Digest()
}

// Created returns the archive creation time.
func (r *InspectResult) Created() time.Time {
	return r.manifest.Created()
}

// FileCount returns the number of files in the archive.
func (r *InspectResult) FileCount() int {
	return r.index.Len()
}

// DataBlobSize returns the size of the data blob (compressed archive).
func (r *InspectResult) DataBlobSize() int64 {
	return r.manifest.DataDescriptor().Size
}

// IndexBlobSize returns the size of the index blob.
func (r *InspectResult) IndexBlobSize() int64 {
	return r.manifest.IndexDescriptor().Size
}

// TotalUncompressedSize returns the sum of all uncompressed file sizes.
// This requires iterating all entries on first call; the result is cached.
func (r *InspectResult) TotalUncompressedSize() uint64 {
	r.computeStats()
	return r.totalUncompressedSize
}

// TotalCompressedSize returns the sum of all compressed file sizes in the data blob.
// This requires iterating all entries on first call; the result is cached.
func (r *InspectResult) TotalCompressedSize() uint64 {
	r.computeStats()
	return r.totalCompressedSize
}

// CompressionRatio returns the ratio of compressed to uncompressed size.
// Returns 1.0 if the archive is uncompressed or has no files.
// This requires iterating all entries on first call; the result is cached.
func (r *InspectResult) CompressionRatio() float64 {
	r.computeStats()
	return r.compressionRatioResult
}

// computeStats computes aggregate statistics by iterating all entries.
func (r *InspectResult) computeStats() {
	r.statsOnce.Do(func() {
		for entry := range r.index.Entries() {
			r.totalUncompressedSize += entry.OriginalSize()
			r.totalCompressedSize += entry.DataSize()
		}
		if r.totalUncompressedSize > 0 {
			r.compressionRatioResult = float64(r.totalCompressedSize) / float64(r.totalUncompressedSize)
		} else {
			r.compressionRatioResult = 1.0
		}
	})
}

// Referrers fetches referrer artifacts (signatures, attestations, etc.).
//
// The artifactType parameter filters referrers by type. Pass "" to get all.
// Returns [registry.ErrReferrersUnsupported] if the registry doesn't support referrers.
func (r *InspectResult) Referrers(ctx context.Context, artifactType string) ([]Referrer, error) {
	regOpts := buildRegistryOpts(r.client)
	regClient := registry.New(regOpts...)

	// Build the subject descriptor from the manifest digest
	dgst, err := digest.Parse(r.manifest.Digest())
	if err != nil {
		return nil, err
	}

	subject := ocispec.Descriptor{
		MediaType: r.manifest.Raw().MediaType,
		Digest:    dgst,
	}

	descs, err := regClient.Referrers(ctx, r.ref, subject, artifactType)
	if err != nil {
		return nil, err
	}

	referrers := make([]Referrer, len(descs))
	for i := range descs {
		referrers[i] = referrerFromDescriptor(&descs[i])
	}
	return referrers, nil
}

// InspectOption configures an Inspect operation.
type InspectOption func(*inspectConfig)

type inspectConfig struct {
	skipCache    bool
	maxIndexSize int64
}

// InspectWithSkipCache bypasses all caches for this inspection.
func InspectWithSkipCache() InspectOption {
	return func(cfg *inspectConfig) {
		cfg.skipCache = true
	}
}

// InspectWithMaxIndexSize sets the maximum index size to fetch.
func InspectWithMaxIndexSize(size int64) InspectOption {
	return func(cfg *inspectConfig) {
		cfg.maxIndexSize = size
	}
}

// Inspect retrieves archive metadata without downloading file data.
//
// This fetches the manifest and index blob, providing access to:
//   - Manifest metadata (digest, created time, annotations)
//   - File index (paths, sizes, hashes, modes)
//   - Archive statistics (file count, total size)
//
// The data blob is NOT downloaded. Use [Client.Pull] to extract files.
//
// Referrers (signatures, attestations) can be fetched on-demand via
// [InspectResult.Referrers].
func (c *Client) Inspect(ctx context.Context, ref string, opts ...InspectOption) (*InspectResult, error) {
	cfg := inspectConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Build registry client options
	regOpts := buildRegistryOpts(c)
	regClient := registry.New(regOpts...)

	// Build inspect options for registry client
	var inspectOpts []registry.InspectOption
	if cfg.skipCache {
		inspectOpts = append(inspectOpts, registry.WithInspectSkipCache())
	}
	if cfg.maxIndexSize > 0 {
		inspectOpts = append(inspectOpts, registry.WithInspectMaxIndexSize(cfg.maxIndexSize))
	}

	// Inspect via registry client
	result, err := regClient.Inspect(ctx, ref, inspectOpts...)
	if err != nil {
		return nil, err
	}

	// Parse index data
	indexView, err := blobcore.NewIndexView(result.IndexData)
	if err != nil {
		return nil, err
	}

	return &InspectResult{
		manifest: result.Manifest,
		index:    indexView,
		client:   c,
		ref:      ref,
	}, nil
}
