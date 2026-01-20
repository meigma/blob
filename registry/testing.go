package registry

import (
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// NewTestManifest creates a BlobManifest for testing purposes.
// This is not intended for production use.
func NewTestManifest(digestStr string, created time.Time, indexSize, dataSize int64) *BlobManifest {
	indexDigest := digest.FromString("test-index-content")
	dataDigest := digest.FromString("test-data-content")

	return &BlobManifest{
		digest: digestStr,
		raw: ocispec.Manifest{
			MediaType:    ocispec.MediaTypeImageManifest,
			ArtifactType: ArtifactType,
			Annotations: map[string]string{
				ocispec.AnnotationCreated: created.Format(time.RFC3339),
			},
			Layers: []ocispec.Descriptor{
				{MediaType: MediaTypeIndex, Size: indexSize, Digest: indexDigest},
				{MediaType: MediaTypeData, Size: dataSize, Digest: dataDigest},
			},
		},
		indexDesc: ocispec.Descriptor{MediaType: MediaTypeIndex, Size: indexSize, Digest: indexDigest},
		dataDesc:  ocispec.Descriptor{MediaType: MediaTypeData, Size: dataSize, Digest: dataDigest},
		created:   created,
	}
}
