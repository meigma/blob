package client

import (
	"context"
	"fmt"
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
	return c.fetchManifestByDigest(ctx, ref, digestStr, cfg.skipCache)
}

// resolveDigest resolves a reference to a digest string.
// Uses ref cache for tags if available, otherwise calls Resolve().
func (c *Client) resolveDigest(ctx context.Context, ref, reference string, skipCache bool) (string, error) {
	// If already a digest, return it directly
	if isDigest(reference) {
		return reference, nil
	}

	// Try ref cache for tag -> digest
	if !skipCache && c.refCache != nil {
		if digest, ok := c.refCache.GetDigest(ref); ok {
			return digest, nil
		}
	}

	// Resolve via network
	desc, err := c.oci.Resolve(ctx, ref, reference)
	if err != nil {
		return "", mapOCIError(err)
	}

	digest := desc.Digest.String()

	// Cache the tag -> digest mapping
	if c.refCache != nil {
		c.refCache.PutDigest(ref, digest)
	}

	return digest, nil
}

// fetchManifestByDigest fetches a manifest by digest.
// Uses manifest cache if available, otherwise calls FetchManifest().
func (c *Client) fetchManifestByDigest(ctx context.Context, ref, digest string, skipCache bool) (*BlobManifest, error) {
	// Try manifest cache
	if !skipCache && c.manifestCache != nil {
		if cached, ok := c.manifestCache.GetManifest(digest); ok {
			return cached, nil
		}
	}

	// Fetch via network
	desc, err := descriptorFromDigest(digest)
	if err != nil {
		return nil, err
	}

	rawManifest, err := c.oci.FetchManifest(ctx, ref, &desc)
	if err != nil {
		return nil, mapOCIError(err)
	}

	manifest, err := parseBlobManifest(&rawManifest, digest)
	if err != nil {
		return nil, err
	}

	// Cache the manifest
	if c.manifestCache != nil {
		c.manifestCache.PutManifest(digest, manifest)
	}

	return manifest, nil
}
