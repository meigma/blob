// Package client provides a high-level client for pushing and pulling
// blob archives to/from OCI registries.
//
// The client uses the oras subpackage for low-level OCI operations and
// adds blob-archive-specific functionality like manifest caching and
// lazy blob access via HTTP range requests.
package client

import (
	"github.com/meigma/blob/client/cache"
	"github.com/meigma/blob/client/oras"
)

// Client provides high-level operations for blob archives in OCI registries.
type Client struct {
	oci           OCIClient
	refCache      cache.RefCache
	manifestCache cache.ManifestCache
	indexCache    cache.IndexCache

	// orasOpts are options passed through to the ORAS client when
	// no custom OCIClient is provided.
	orasOpts []oras.Option
}

// New creates a new blob archive client with the given options.
//
// If no OCIClient is provided via WithOCIClient, a default ORAS-based
// client is created using any pass-through options (WithPlainHTTP, etc.).
func New(opts ...Option) *Client {
	c := &Client{}
	for _, opt := range opts {
		opt(c)
	}

	// Create default ORAS client if none provided
	if c.oci == nil {
		c.oci = oras.New(c.orasOpts...)
	}

	return c
}
