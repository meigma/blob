package client

import "context"

// Fetch retrieves the manifest for a blob archive without downloading data.
//
// This is useful for inspecting archive metadata or checking if an archive
// exists without the overhead of downloading blob content.
func (c *Client) Fetch(ctx context.Context, ref string, opts ...FetchOption) (*BlobManifest, error) {
	panic("not implemented")
}
