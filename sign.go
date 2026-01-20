package blob

import (
	"context"

	"github.com/meigma/blob/registry"
)

// ManifestSigner signs OCI manifest payloads.
type ManifestSigner = registry.ManifestSigner

// Sign creates a sigstore signature for a manifest and attaches it as a referrer.
//
// The ref must include a tag or digest. The signer creates the signature bundle,
// which is pushed as an OCI 1.1 referrer artifact linked to the manifest.
//
// Returns the digest of the signature manifest.
func (c *Client) Sign(ctx context.Context, ref string, signer ManifestSigner, opts ...SignOption) (string, error) {
	// Build registry client options
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

	regClient := registry.New(regOpts...)

	// Build sign options
	var signOpts []registry.SignOption
	_ = signOpts // reserved for future options

	return regClient.Sign(ctx, ref, signer, signOpts...)
}
