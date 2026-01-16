package client

import (
	"github.com/meigma/blob/client/oras"
)

// Option configures a Client.
type Option func(*Client)

// WithOCIClient sets a custom OCI client.
// If not set, a default ORAS-based client is created.
//
// When a custom OCIClient is provided, pass-through options like
// WithPlainHTTP and WithDockerConfig are ignored.
func WithOCIClient(c OCIClient) Option {
	return func(client *Client) {
		client.oci = c
	}
}

// WithPlainHTTP enables plain HTTP (no TLS) for registries.
// This is passed through to the default ORAS client.
func WithPlainHTTP(enabled bool) Option {
	return func(c *Client) {
		c.orasOpts = append(c.orasOpts, oras.WithPlainHTTP(enabled))
	}
}

// WithDockerConfig enables reading credentials from ~/.docker/config.json.
// This is passed through to the default ORAS client.
func WithDockerConfig() Option {
	return func(c *Client) {
		c.orasOpts = append(c.orasOpts, oras.WithDockerConfig())
	}
}

// WithUserAgent sets the User-Agent header for requests.
// This is passed through to the default ORAS client.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.orasOpts = append(c.orasOpts, oras.WithUserAgent(ua))
	}
}

// WithRefCache sets the cache for reference to digest mappings.
func WithRefCache(cache RefCache) Option {
	return func(c *Client) {
		c.refCache = cache
	}
}

// WithManifestCache sets the cache for manifest lookups.
func WithManifestCache(cache ManifestCache) Option {
	return func(c *Client) {
		c.manifestCache = cache
	}
}
