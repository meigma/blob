package registry

import (
	"fmt"
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
func parseBlobManifest(manifest *ocispec.Manifest, digest string) (*BlobManifest, error) {
	if manifest.MediaType != ocispec.MediaTypeImageManifest {
		return nil, fmt.Errorf("%w: unexpected manifest media type %q", ErrInvalidManifest, manifest.MediaType)
	}
	if manifest.ArtifactType != ArtifactType {
		return nil, fmt.Errorf("%w: unexpected artifact type %q", ErrInvalidManifest, manifest.ArtifactType)
	}

	var indexDesc, dataDesc ocispec.Descriptor
	var foundIndex, foundData bool

	for _, layer := range manifest.Layers {
		switch layer.MediaType {
		case MediaTypeIndex:
			if foundIndex {
				return nil, fmt.Errorf("%w: multiple index layers", ErrInvalidManifest)
			}
			indexDesc = layer
			foundIndex = true
		case MediaTypeData:
			if foundData {
				return nil, fmt.Errorf("%w: multiple data layers", ErrInvalidManifest)
			}
			dataDesc = layer
			foundData = true
		}
	}

	if !foundIndex {
		return nil, ErrMissingIndex
	}
	if !foundData {
		return nil, ErrMissingData
	}
	if len(manifest.Layers) != 2 {
		return nil, fmt.Errorf("%w: expected 2 layers, got %d", ErrInvalidManifest, len(manifest.Layers))
	}

	var created time.Time
	if ts, ok := manifest.Annotations[ocispec.AnnotationCreated]; ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			created = t
		}
	}

	return &BlobManifest{
		raw:       *manifest,
		digest:    digest,
		indexDesc: indexDesc,
		dataDesc:  dataDesc,
		created:   created,
	}, nil
}
