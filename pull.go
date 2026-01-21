package blob

import (
	"context"

	blobcore "github.com/meigma/blob/core"
	"github.com/meigma/blob/registry"
)

// Archive wraps a pulled blob archive with integrated caching.
// It embeds *core.Blob, so all Blob methods are directly accessible.
type Archive struct {
	*blobcore.Blob
}

// Pull retrieves an archive from the registry with lazy data loading.
//
// The returned Archive wraps a core.Blob with caching support. File data
// is fetched on demand via HTTP range requests.
//
// Use [PullWithSkipCache] to bypass all caches and force a fresh fetch.
func (c *Client) Pull(ctx context.Context, ref string, opts ...PullOption) (*Archive, error) {
	cfg := pullConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	c.log().Info("pulling from registry", "ref", ref)

	// Build registry client options
	regOpts := buildRegistryOpts(c)

	regClient := registry.New(regOpts...)

	// Build pull options for registry client
	var pullOpts []registry.PullOption
	if cfg.skipCache {
		pullOpts = append(pullOpts, registry.WithPullSkipCache())
	}
	if cfg.maxIndexSize > 0 {
		pullOpts = append(pullOpts, registry.WithMaxIndexSize(cfg.maxIndexSize))
	}

	// Pass through blob options
	blobOpts := cfg.blobOpts

	// Add content cache if configured
	if c.contentCache != nil {
		blobOpts = append(blobOpts, blobcore.WithCache(c.contentCache))
	}

	// Propagate logger to blob
	if c.logger != nil {
		blobOpts = append(blobOpts, blobcore.WithLogger(c.logger))
	}

	// Pass blob options to pull
	if len(blobOpts) > 0 {
		pullOpts = append(pullOpts, registry.WithBlobOptions(blobOpts...))
	}

	// Pull via registry client
	blob, err := regClient.Pull(ctx, ref, pullOpts...)
	if err != nil {
		return nil, err
	}

	return &Archive{Blob: blob}, nil
}

// buildRegistryOpts creates registry.Option slice from Client configuration.
func buildRegistryOpts(c *Client) []registry.Option {
	var regOpts []registry.Option //nolint:prealloc // size depends on optional config
	regOpts = append(regOpts, registry.WithOrasOptions(c.orasOpts...))
	if c.refCache != nil {
		regOpts = append(regOpts, registry.WithRefCache(c.refCache))
	}
	if c.manifestCache != nil {
		regOpts = append(regOpts, registry.WithManifestCache(c.manifestCache))
	}
	if c.indexCache != nil {
		regOpts = append(regOpts, registry.WithIndexCache(c.indexCache))
	}
	for _, p := range c.policies {
		regOpts = append(regOpts, registry.WithPolicy(p))
	}
	if c.logger != nil {
		regOpts = append(regOpts, registry.WithLogger(c.logger))
	}
	return regOpts
}
