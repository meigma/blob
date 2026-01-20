// Package blob provides a file archive format optimized for random access
// via HTTP range requests against OCI registries.
//
// This package provides a unified high-level API through [Client] for pushing
// and pulling blob archives to/from OCI registries. For low-level archive
// operations without registry interaction, use the [core] subpackage.
//
// Archives consist of two OCI blobs:
//   - Index blob: FlatBuffers-encoded file metadata enabling O(log n) lookups
//   - Data blob: Concatenated file contents, sorted by path for efficient directory fetches
//
// # Quick Start
//
// Push a directory to a registry:
//
//	c, err := blob.NewClient(blob.WithDockerConfig())
//	if err != nil {
//	    return err
//	}
//	err = c.Push(ctx, "ghcr.io/myorg/myarchive:v1", "./src",
//	    blob.PushWithCompression(blob.CompressionZstd),
//	)
//
// Pull and read files:
//
//	archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
//	if err != nil {
//	    return err
//	}
//	content, err := archive.ReadFile("config.json")
//
// # Caching
//
// Use WithCacheDir for automatic caching of all blob metadata and content:
//
//	c, err := blob.NewClient(
//	    blob.WithDockerConfig(),
//	    blob.WithCacheDir("/var/cache/blob"),
//	)
//
// For fine-grained control, use individual cache options like
// [WithRefCacheDir], [WithManifestCacheDir], [WithContentCacheDir], etc.
//
// # Policies
//
// Add verification policies to enforce security requirements on pull:
//
//	sigPolicy, _ := sigstore.NewPolicy(sigstore.WithIdentity(issuer, subject))
//	c, err := blob.NewClient(
//	    blob.WithDockerConfig(),
//	    blob.WithPolicy(sigPolicy),
//	)
package blob

import (
	"time"

	corecache "github.com/meigma/blob/core/cache"
	"github.com/meigma/blob/registry"
	registrycache "github.com/meigma/blob/registry/cache"
	"github.com/meigma/blob/registry/oras"
)

// Client provides high-level operations for blob archives in OCI registries.
//
// Client wraps a registry client and adds blob-archive-specific functionality
// including automatic archive creation, content caching, and policy enforcement.
type Client struct {
	// orasOpts are options for the underlying ORAS client.
	orasOpts []oras.Option

	// Caches
	contentCache  corecache.Cache             // core/cache - for file content
	blockCache    corecache.BlockCache        // core/cache - for HTTP range optimization
	refCache      registrycache.RefCache      // registry/cache - tag→digest
	manifestCache registrycache.ManifestCache // registry/cache - digest→manifest
	indexCache    registrycache.IndexCache    // registry/cache - digest→index bytes
	refCacheTTL   time.Duration               // TTL for ref cache entries

	// Policies
	policies []Policy
}

// NewClient creates a new blob archive client with the given options.
//
// If no authentication is configured, anonymous access is used.
// Use [WithDockerConfig] to read credentials from ~/.docker/config.json.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Policy evaluates whether a manifest is trusted.
//
// Policies are evaluated during Fetch and Pull operations. If any policy
// returns an error, the operation fails with [ErrPolicyViolation].
type Policy = registry.Policy

// PolicyRequest provides context for policy evaluation.
type PolicyRequest = registry.PolicyRequest

// PolicyClient exposes minimal client capabilities for policies.
type PolicyClient = registry.PolicyClient
