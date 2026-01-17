// Package cache provides caching interfaces for the blob client.
package cache

import (
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// RefCache caches reference to digest mappings.
//
// This avoids redundant HEAD requests for tag resolution.
type RefCache interface {
	// GetDigest returns the digest for a reference if cached.
	GetDigest(ref string) (digest string, ok bool)

	// PutDigest caches a reference to digest mapping.
	PutDigest(ref string, digest string) error
}

// ManifestCache caches digest to manifest mappings.
//
// This avoids redundant manifest fetches.
type ManifestCache interface {
	// GetManifest returns the cached manifest for a digest.
	GetManifest(digest string) (manifest *ocispec.Manifest, ok bool)

	// PutManifest caches a manifest by its digest.
	PutManifest(digest string, manifest *ocispec.Manifest) error
}
