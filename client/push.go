package client

import (
	"context"

	"github.com/meigma/blob"
)

// Push pushes a blob archive to an OCI registry.
//
// The archive is pushed as two blobs (index and data) with a manifest
// linking them. The ref must include a tag (e.g., "registry.com/repo:v1.0.0").
//
// Use WithTags to apply additional tags to the same manifest.
func (c *Client) Push(ctx context.Context, ref string, b *blob.Blob, opts ...PushOption) error {
	panic("not implemented")
}
