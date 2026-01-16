package client

// RefCache caches reference to digest mappings.
//
// This avoids redundant HEAD requests for tag resolution.
type RefCache interface {
	// GetDigest returns the digest for a reference if cached.
	GetDigest(ref string) (digest string, ok bool)

	// PutDigest caches a reference to digest mapping.
	PutDigest(ref string, digest string)
}

// ManifestCache caches digest to manifest mappings.
//
// This avoids redundant manifest fetches.
type ManifestCache interface {
	// GetManifest returns the cached manifest for a digest.
	GetManifest(digest string) (manifest *BlobManifest, ok bool)

	// PutManifest caches a manifest by its digest.
	PutManifest(digest string, manifest *BlobManifest)
}
