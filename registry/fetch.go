package registry

import (
	"context"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Fetch retrieves the manifest for a blob archive without downloading data.
//
// This is useful for inspecting archive metadata or checking if an archive
// exists without the overhead of downloading blob content.
func (c *Client) Fetch(ctx context.Context, ref string, opts ...FetchOption) (*BlobManifest, error) {
	cfg := fetchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	parsedRef, err := parseClientRef(ref)
	if err != nil {
		return nil, err
	}
	if parsedRef.reference == "" {
		return nil, fmt.Errorf("%w: reference must include a tag or digest", ErrInvalidReference)
	}

	// Step 1: Resolve to digest (cache or network)
	digestStr, err := c.resolveDigest(ctx, ref, parsedRef.reference, cfg.skipCache)
	if err != nil {
		return nil, err
	}

	// Step 2: Get manifest by digest (cache or network)
	manifest, raw, fromCache, err := c.fetchManifestByDigest(ctx, ref, digestStr, cfg.skipCache)
	if err != nil {
		return nil, err
	}

	if err := c.evaluatePolicies(ctx, ref, digestStr, manifest, raw); err != nil {
		if fromCache && c.manifestCache != nil {
			_ = c.manifestCache.Delete(digestStr) //nolint:errcheck // best-effort cleanup
		}
		return nil, err
	}

	if !fromCache && c.manifestCache != nil && !cfg.skipCache {
		if err := c.manifestCache.PutManifest(digestStr, raw); err != nil {
			return nil, fmt.Errorf("cache manifest: %w", err)
		}
	}

	return manifest, nil
}

// resolveDigest resolves a reference to a digest string.
// Uses ref cache for tags if available, otherwise calls Resolve().
func (c *Client) resolveDigest(ctx context.Context, ref, reference string, skipCache bool) (string, error) {
	// If already a digest, return it directly
	if isDigest(reference) {
		c.log().Debug("resolving reference", "ref", ref, "type", "digest")
		return reference, nil
	}

	c.log().Debug("resolving reference", "ref", ref, "type", "tag")

	// Try ref cache for tag -> digest
	if !skipCache && c.refCache != nil {
		if digest, ok := c.refCache.GetDigest(ref); ok {
			c.log().Debug("ref cache hit", "ref", ref, "digest", digest[:min(16, len(digest))])
			return digest, nil
		}
		c.log().Debug("ref cache miss", "ref", ref)
	}

	// Resolve via network
	desc, err := c.oci.Resolve(ctx, ref, reference)
	if err != nil {
		return "", mapOCIError(err)
	}

	digest := desc.Digest.String()

	// Cache the tag -> digest mapping
	if c.refCache != nil {
		if err := c.refCache.PutDigest(ref, digest); err != nil {
			return "", fmt.Errorf("cache ref digest: %w", err)
		}
	}

	return digest, nil
}

// fetchManifestByDigest fetches a manifest by digest.
// Uses manifest cache if available, otherwise calls FetchManifest().
func (c *Client) fetchManifestByDigest(ctx context.Context, ref, dgst string, skipCache bool) (manifest *BlobManifest, raw []byte, fromCache bool, err error) {
	// Try manifest cache
	if !skipCache && c.manifestCache != nil {
		if cached, cachedRaw, ok := c.manifestCache.GetManifest(dgst); ok {
			c.log().Debug("manifest cache hit", "digest", dgst[:min(16, len(dgst))])
			manifest, err = parseBlobManifest(cached, dgst)
			return manifest, cachedRaw, true, err
		}
		c.log().Debug("manifest cache miss", "digest", dgst[:min(16, len(dgst))])
	}

	// Fetch via network
	var desc ocispec.Descriptor
	desc, err = descriptorFromDigest(dgst)
	if err != nil {
		return nil, nil, false, err
	}

	var rawManifest ocispec.Manifest
	rawManifest, raw, err = c.oci.FetchManifest(ctx, ref, &desc)
	if err != nil {
		return nil, nil, false, mapOCIError(err)
	}

	manifest, err = parseBlobManifest(&rawManifest, dgst)
	if err != nil {
		return nil, nil, false, err
	}
	return manifest, raw, false, nil
}
