package registry

import (
	"log/slog"

	"github.com/meigma/blob/registry/cache"
	"github.com/meigma/blob/registry/oras"
)

// Client provides high-level operations for blob archives in OCI registries.
type Client struct {
	oci           OCIClient
	refCache      cache.RefCache
	manifestCache cache.ManifestCache
	indexCache    cache.IndexCache
	policies      []Policy
	logger        *slog.Logger

	// orasOpts are options passed through to the ORAS client when
	// no custom OCIClient is provided.
	orasOpts []oras.Option
}

// log returns the logger, falling back to a discard logger if nil.
func (c *Client) log() *slog.Logger {
	if c.logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.logger
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
		// Propagate logger to ORAS client
		orasOpts := c.orasOpts
		if c.logger != nil {
			orasOpts = append(orasOpts, oras.WithLogger(c.logger))
		}
		c.oci = oras.New(orasOpts...)
	}

	return c
}
