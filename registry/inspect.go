package registry

import (
	"context"
)

// InspectResult contains manifest and index metadata without the data blob.
type InspectResult struct {
	// Manifest contains the parsed OCI manifest for the blob archive.
	Manifest *BlobManifest

	// IndexData contains the raw FlatBuffers-encoded index blob.
	IndexData []byte
}

// Inspect retrieves archive metadata without downloading file data.
//
// This fetches the manifest and index blob, providing access to:
//   - Manifest metadata (digest, created time, annotations)
//   - File index (paths, sizes, hashes, modes)
//
// The data blob is NOT downloaded. Use [Client.Pull] to extract files.
func (c *Client) Inspect(ctx context.Context, ref string, opts ...InspectOption) (*InspectResult, error) {
	cfg := inspectConfig{
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
	// Reuse the pull config structure for fetchIndexBlob
	pullCfg := &pullConfig{
		skipCache:    cfg.skipCache,
		maxIndexSize: cfg.maxIndexSize,
	}
	indexData, err := c.fetchIndexBlob(ctx, ref, manifest, pullCfg)
	if err != nil {
		return nil, err
	}

	return &InspectResult{
		Manifest:  manifest,
		IndexData: indexData,
	}, nil
}
