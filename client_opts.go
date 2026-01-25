package blob

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	corecache "github.com/meigma/blob/core/cache"
	coredisk "github.com/meigma/blob/core/cache/disk"
	registrycache "github.com/meigma/blob/registry/cache"
	registrydisk "github.com/meigma/blob/registry/cache/disk"
	"github.com/meigma/blob/registry/oras"
)

// Option configures a Client.
type Option func(*Client) error

// Default cache size limits for WithCacheDir.
const (
	DefaultContentCacheSize  int64 = 100 << 20 // 100 MB
	DefaultBlockCacheSize    int64 = 50 << 20  // 50 MB
	DefaultIndexCacheSize    int64 = 50 << 20  // 50 MB
	DefaultManifestCacheSize int64 = 10 << 20  // 10 MB
	DefaultRefCacheSize      int64 = 5 << 20   // 5 MB
	DefaultRefCacheTTL             = 5 * time.Minute
)

// --- Authentication Options ---

// WithDockerConfig enables reading credentials from ~/.docker/config.json.
// This is the recommended way to authenticate with registries.
func WithDockerConfig() Option {
	return func(c *Client) error {
		c.orasOpts = append(c.orasOpts, oras.WithDockerConfig())
		return nil
	}
}

// WithStaticCredentials sets static username/password credentials for a registry.
// The registry parameter should be the registry host (e.g., "ghcr.io").
func WithStaticCredentials(registry, username, password string) Option {
	return func(c *Client) error {
		c.orasOpts = append(c.orasOpts, oras.WithStaticCredentials(registry, username, password))
		return nil
	}
}

// WithStaticToken sets a static bearer token for a registry.
// The registry parameter should be the registry host (e.g., "ghcr.io").
func WithStaticToken(registry, token string) Option {
	return func(c *Client) error {
		c.orasOpts = append(c.orasOpts, oras.WithStaticToken(registry, token))
		return nil
	}
}

// WithAnonymous forces anonymous access, ignoring any configured credentials.
func WithAnonymous() Option {
	return func(c *Client) error {
		c.orasOpts = append(c.orasOpts, oras.WithAnonymous())
		return nil
	}
}

// --- Transport Options ---

// WithPlainHTTP enables plain HTTP (no TLS) for registries.
// This is useful for local development registries.
func WithPlainHTTP(enabled bool) Option {
	return func(c *Client) error {
		c.orasOpts = append(c.orasOpts, oras.WithPlainHTTP(enabled))
		return nil
	}
}

// WithUserAgent sets the User-Agent header for registry requests.
func WithUserAgent(ua string) Option {
	return func(c *Client) error {
		c.orasOpts = append(c.orasOpts, oras.WithUserAgent(ua))
		return nil
	}
}

// --- Caching Options (Simple) ---

// WithCacheDir enables all caches with default sizes in subdirectories of dir.
//
// This creates:
//   - dir/content/ - file content cache (100 MB)
//   - dir/blocks/  - HTTP range block cache (50 MB)
//   - dir/refs/    - tag→digest cache (5 MB)
//   - dir/manifests/ - manifest cache (10 MB)
//   - dir/indexes/ - index blob cache (50 MB)
//
// For custom sizes or selective caching, use individual cache options.
func WithCacheDir(dir string) Option {
	return func(c *Client) error {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}

		// Content cache
		contentCache, err := coredisk.New(
			filepath.Join(dir, "content"),
			coredisk.WithMaxBytes(DefaultContentCacheSize),
		)
		if err != nil {
			return err
		}
		c.contentCache = contentCache

		// Block cache
		blockCache, err := coredisk.NewBlockCache(
			filepath.Join(dir, "blocks"),
			coredisk.WithBlockMaxBytes(DefaultBlockCacheSize),
		)
		if err != nil {
			return err
		}
		c.blockCache = blockCache

		// Ref cache
		refCache, err := registrydisk.NewRefCache(
			filepath.Join(dir, "refs"),
			registrydisk.WithMaxBytes(DefaultRefCacheSize),
			registrydisk.WithRefCacheTTL(c.refCacheTTL),
		)
		if err != nil {
			return err
		}
		c.refCache = refCache

		// Manifest cache
		manifestCache, err := registrydisk.NewManifestCache(
			filepath.Join(dir, "manifests"),
			registrydisk.WithMaxBytes(DefaultManifestCacheSize),
		)
		if err != nil {
			return err
		}
		c.manifestCache = manifestCache

		// Index cache
		indexCache, err := registrydisk.NewIndexCache(
			filepath.Join(dir, "indexes"),
			registrydisk.WithMaxBytes(DefaultIndexCacheSize),
		)
		if err != nil {
			return err
		}
		c.indexCache = indexCache

		return nil
	}
}

// WithContentCacheDir enables file content caching in the specified directory
// with the default size limit ([DefaultContentCacheSize]).
func WithContentCacheDir(dir string) Option {
	return func(c *Client) error {
		cache, err := coredisk.New(dir, coredisk.WithMaxBytes(DefaultContentCacheSize))
		if err != nil {
			return err
		}
		c.contentCache = cache
		return nil
	}
}

// WithBlockCacheDir enables HTTP range block caching in the specified directory
// with the default size limit ([DefaultBlockCacheSize]).
func WithBlockCacheDir(dir string) Option {
	return func(c *Client) error {
		cache, err := coredisk.NewBlockCache(dir, coredisk.WithBlockMaxBytes(DefaultBlockCacheSize))
		if err != nil {
			return err
		}
		c.blockCache = cache
		return nil
	}
}

// WithRefCacheDir enables tag-to-digest reference caching in the specified directory
// with the default size limit ([DefaultRefCacheSize]).
//
// The cache uses the TTL configured via [WithRefCacheTTL], which defaults to
// [DefaultRefCacheTTL] (5 minutes). Set [WithRefCacheTTL] before this option
// to customize the TTL.
func WithRefCacheDir(dir string) Option {
	return func(c *Client) error {
		cache, err := registrydisk.NewRefCache(dir,
			registrydisk.WithMaxBytes(DefaultRefCacheSize),
			registrydisk.WithRefCacheTTL(c.refCacheTTL),
		)
		if err != nil {
			return err
		}
		c.refCache = cache
		return nil
	}
}

// WithManifestCacheDir enables manifest caching in the specified directory
// with the default size limit ([DefaultManifestCacheSize]).
func WithManifestCacheDir(dir string) Option {
	return func(c *Client) error {
		cache, err := registrydisk.NewManifestCache(dir, registrydisk.WithMaxBytes(DefaultManifestCacheSize))
		if err != nil {
			return err
		}
		c.manifestCache = cache
		return nil
	}
}

// WithIndexCacheDir enables index blob caching in the specified directory
// with the default size limit ([DefaultIndexCacheSize]).
func WithIndexCacheDir(dir string) Option {
	return func(c *Client) error {
		cache, err := registrydisk.NewIndexCache(dir, registrydisk.WithMaxBytes(DefaultIndexCacheSize))
		if err != nil {
			return err
		}
		c.indexCache = cache
		return nil
	}
}

// WithRefCacheTTL sets the TTL for reference cache entries.
// This determines how long tag→digest mappings are considered fresh.
// Use 0 to disable TTL expiration. Negative values are not allowed.
//
// This option must be set before [WithCacheDir] or [WithRefCacheDir]
// to take effect, as the TTL is applied when the cache is created.
func WithRefCacheTTL(ttl time.Duration) Option {
	return func(c *Client) error {
		if ttl < 0 {
			return errors.New("ref cache TTL must be non-negative")
		}
		c.refCacheTTL = ttl
		return nil
	}
}

// --- Caching Options (Advanced) ---

// WithContentCache sets a custom content cache implementation.
// Import github.com/meigma/blob/core/cache/disk for the disk implementation.
func WithContentCache(cache corecache.Cache) Option {
	return func(c *Client) error {
		c.contentCache = cache
		return nil
	}
}

// WithBlockCache sets a custom block cache implementation.
// Import github.com/meigma/blob/core/cache/disk for the disk implementation.
func WithBlockCache(cache corecache.BlockCache) Option {
	return func(c *Client) error {
		c.blockCache = cache
		return nil
	}
}

// WithRefCache sets a custom reference cache implementation.
// Import github.com/meigma/blob/registry/cache/disk for the disk implementation.
func WithRefCache(cache registrycache.RefCache) Option {
	return func(c *Client) error {
		c.refCache = cache
		return nil
	}
}

// WithManifestCache sets a custom manifest cache implementation.
// Import github.com/meigma/blob/registry/cache/disk for the disk implementation.
func WithManifestCache(cache registrycache.ManifestCache) Option {
	return func(c *Client) error {
		c.manifestCache = cache
		return nil
	}
}

// WithIndexCache sets a custom index cache implementation.
// Import github.com/meigma/blob/registry/cache/disk for the disk implementation.
func WithIndexCache(cache registrycache.IndexCache) Option {
	return func(c *Client) error {
		c.indexCache = cache
		return nil
	}
}

// --- Policy Options ---

// WithPolicy adds a policy that must pass for Fetch and Pull operations.
// Policies are evaluated in order; the first failure stops evaluation.
func WithPolicy(policy Policy) Option {
	return func(c *Client) error {
		if policy != nil {
			c.policies = append(c.policies, policy)
		}
		return nil
	}
}

// WithPolicies adds multiple policies that must pass for Fetch and Pull operations.
func WithPolicies(policies ...Policy) Option {
	return func(c *Client) error {
		for _, p := range policies {
			if p != nil {
				c.policies = append(c.policies, p)
			}
		}
		return nil
	}
}

// WithLogger sets a logger for the client.
// The logger is propagated to the underlying registry client.
// If nil, a discard logger is used (default behavior).
func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) error {
		c.logger = logger
		return nil
	}
}
