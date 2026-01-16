package oci

import (
	"time"

	"oras.land/oras-go/v2/registry/remote/credentials"
)

// Option configures an OCI Client.
type Option func(*Client)

// WithCredentialStore sets the credential store for authentication.
func WithCredentialStore(store credentials.Store) Option {
	return func(c *Client) {
		c.credStore = store
	}
}

// WithStaticCredentials sets static username/password credentials for a registry.
func WithStaticCredentials(registry, username, password string) Option {
	return func(c *Client) {
		c.credStore = StaticCredentials(registry, username, password)
	}
}

// WithStaticToken sets a bearer token for a registry.
func WithStaticToken(registry, token string) Option {
	return func(c *Client) {
		c.credStore = StaticToken(registry, token)
	}
}

// WithDockerConfig enables reading credentials from ~/.docker/config.json.
// If the docker config cannot be loaded (common in environments without docker),
// the client falls back to no credentials.
func WithDockerConfig() Option {
	return func(c *Client) {
		store, err := DefaultCredentialStore()
		if err != nil {
			return
		}
		c.credStore = store
	}
}

// WithPlainHTTP enables plain HTTP (no TLS) for registries.
// This is useful for local development registries.
func WithPlainHTTP(enabled bool) Option {
	return func(c *Client) {
		c.plainHTTP = enabled
	}
}

// WithAnonymous disables all authentication, including credential store lookups.
// Use this for public registries where authentication is not needed.
func WithAnonymous() Option {
	return func(c *Client) {
		c.anonymous = true
	}
}

// WithUserAgent sets the User-Agent header for requests.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithAuthHeaderCacheTTL sets the TTL for cached auth headers.
// Use a zero or negative duration to disable caching.
func WithAuthHeaderCacheTTL(ttl time.Duration) Option {
	return func(c *Client) {
		c.authHeaderCache = newAuthHeaderCache(ttl)
	}
}
