package blob

import (
	"context"

	"github.com/meigma/blob/registry"
)

// Manifest represents a blob archive manifest from an OCI registry.
type Manifest = registry.BlobManifest

// FetchOption configures a Fetch operation.
type FetchOption func(*fetchConfig)

type fetchConfig struct {
	skipCache bool
}

// FetchWithSkipCache bypasses the ref and manifest caches for this fetch.
//
// The fetched manifest is still added to the cache after retrieval.
func FetchWithSkipCache() FetchOption {
	return func(cfg *fetchConfig) {
		cfg.skipCache = true
	}
}

// Fetch retrieves the manifest for a blob archive without downloading data.
//
// This is useful for inspecting archive metadata or checking if an archive
// exists without the overhead of downloading blob content.
func (c *Client) Fetch(ctx context.Context, ref string, opts ...FetchOption) (*Manifest, error) {
	cfg := fetchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	c.log().Debug("fetching manifest", "ref", ref)

	// Build registry client options
	regOpts := buildRegistryOpts(c)

	regClient := registry.New(regOpts...)

	// Build fetch options
	var fetchOpts []registry.FetchOption
	if cfg.skipCache {
		fetchOpts = append(fetchOpts, registry.WithSkipCache())
	}

	return regClient.Fetch(ctx, ref, fetchOpts...)
}

// Tag creates or updates a tag pointing to an existing manifest.
//
// The ref specifies the repository and new tag (e.g., "registry.com/repo:latest").
// The digest must be the full digest of an existing manifest (e.g., "sha256:abc...").
func (c *Client) Tag(ctx context.Context, ref, digest string) error {
	// Build registry client options
	regOpts := buildRegistryOpts(c)

	regClient := registry.New(regOpts...)

	return regClient.Tag(ctx, ref, digest)
}
