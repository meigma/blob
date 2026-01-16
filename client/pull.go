package client

import (
	"context"

	"github.com/meigma/blob"
)

// Pull retrieves a blob archive from an OCI registry.
//
// The returned Blob is lazy: file data is fetched on demand via HTTP range
// requests. The index blob is downloaded immediately as it is small.
//
// The caller should close the Blob when done if it wraps file resources.
func (c *Client) Pull(ctx context.Context, ref string, opts ...PullOption) (*blob.Blob, error) {
	panic("not implemented")
}
