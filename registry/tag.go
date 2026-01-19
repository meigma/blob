package registry

import (
	"context"
	"fmt"
)

// Tag creates or updates a tag pointing to an existing manifest.
//
// The ref specifies the repository and new tag (e.g., "registry.com/repo:latest").
// The digest must be the full digest of an existing manifest (e.g., "sha256:abc...").
func (c *Client) Tag(ctx context.Context, ref, digest string) error {
	parsedRef, err := parseClientRef(ref)
	if err != nil {
		return err
	}

	tag := parsedRef.reference
	if tag == "" || isDigest(tag) {
		return fmt.Errorf("%w: reference must include a tag", ErrInvalidReference)
	}

	// Resolve the digest to get the full descriptor (including media type).
	// This is required because ORAS needs the media type to fetch the manifest
	// when tagging.
	desc, err := c.oci.Resolve(ctx, ref, digest)
	if err != nil {
		return mapOCIError(err)
	}

	return mapOCIError(c.oci.Tag(ctx, ref, &desc, tag))
}
