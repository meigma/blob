package client

import (
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// BlobManifest wraps an OCI manifest for a blob archive.
//
// It provides convenient access to the index and data blob descriptors,
// annotations, and other metadata.
type BlobManifest struct {
	raw       ocispec.Manifest
	digest    string
	indexDesc ocispec.Descriptor
	dataDesc  ocispec.Descriptor
	created   time.Time
}

// IndexDescriptor returns the descriptor for the index blob.
func (m *BlobManifest) IndexDescriptor() ocispec.Descriptor {
	return m.indexDesc
}

// DataDescriptor returns the descriptor for the data blob.
func (m *BlobManifest) DataDescriptor() ocispec.Descriptor {
	return m.dataDesc
}

// Digest returns the manifest digest.
func (m *BlobManifest) Digest() string {
	return m.digest
}

// Annotations returns the manifest annotations.
func (m *BlobManifest) Annotations() map[string]string {
	return m.raw.Annotations
}

// Created returns the creation timestamp from annotations.
//
// Returns zero time if the annotation is not present or cannot be parsed.
func (m *BlobManifest) Created() time.Time {
	return m.created
}

// Raw returns the underlying OCI manifest.
func (m *BlobManifest) Raw() ocispec.Manifest {
	return m.raw
}

// parseBlobManifest parses an OCI manifest into a BlobManifest.
func parseBlobManifest(manifest ocispec.Manifest, digest string) (*BlobManifest, error) {
	panic("not implemented")
}
