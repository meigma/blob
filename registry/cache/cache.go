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

	// Delete removes a cached reference.
	Delete(ref string) error

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes cached entries until the cache is at or below targetBytes.
	// Returns the number of bytes freed.
	Prune(targetBytes int64) (int64, error)
}

// ManifestCache caches digest to manifest mappings.
//
// This avoids redundant manifest fetches.
type ManifestCache interface {
	// GetManifest returns the cached manifest for a digest.
	GetManifest(digest string) (manifest *ocispec.Manifest, ok bool)

	// PutManifest caches raw manifest bytes by digest.
	PutManifest(digest string, raw []byte) error

	// Delete removes a cached manifest.
	Delete(digest string) error

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes cached entries until the cache is at or below targetBytes.
	// Returns the number of bytes freed.
	Prune(targetBytes int64) (int64, error)
}

// IndexCache caches digest to index blob mappings.
//
// This avoids redundant index blob fetches.
type IndexCache interface {
	// GetIndex returns the cached index bytes for a digest.
	GetIndex(digest string) (index []byte, ok bool)

	// PutIndex caches raw index bytes by digest.
	PutIndex(digest string, raw []byte) error

	// Delete removes a cached index blob.
	Delete(digest string) error

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes cached entries until the cache is at or below targetBytes.
	// Returns the number of bytes freed.
	Prune(targetBytes int64) (int64, error)
}
