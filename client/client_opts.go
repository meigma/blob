package client

import (
	"net/http"

	"github.com/meigma/blob/client/oci"
)

// Option configures a Client.
type Option func(*Client)

// WithOCIClient sets a custom OCI client.
// If not set, a default OCI client is created.
func WithOCIClient(c *oci.Client) Option {
	panic("not implemented")
}

// WithHTTPClient sets the HTTP client used for requests.
// This is passed through to the OCI client.
func WithHTTPClient(client *http.Client) Option {
	panic("not implemented")
}

// WithCredentials sets static username/password credentials for a registry.
// This is passed through to the OCI client.
func WithCredentials(registry, username, password string) Option {
	panic("not implemented")
}

// WithToken sets a bearer token for a registry.
// This is passed through to the OCI client.
func WithToken(registry, token string) Option {
	panic("not implemented")
}

// WithDockerConfig enables reading credentials from ~/.docker/config.json.
// This is passed through to the OCI client.
func WithDockerConfig() Option {
	panic("not implemented")
}

// WithPlainHTTP enables plain HTTP (no TLS) for registries.
// This is passed through to the OCI client.
func WithPlainHTTP(enabled bool) Option {
	panic("not implemented")
}

// WithRefCache sets the cache for reference to digest mappings.
func WithRefCache(cache RefCache) Option {
	panic("not implemented")
}

// WithManifestCache sets the cache for manifest lookups.
func WithManifestCache(cache ManifestCache) Option {
	panic("not implemented")
}
