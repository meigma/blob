package client

import "context"

// Tag creates or updates a tag pointing to an existing manifest.
//
// The ref specifies the repository and new tag (e.g., "registry.com/repo:latest").
// The digest must be the full digest of an existing manifest (e.g., "sha256:abc...").
func (c *Client) Tag(ctx context.Context, ref string, digest string) error {
	panic("not implemented")
}
