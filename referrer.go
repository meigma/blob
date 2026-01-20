package blob

import ocispec "github.com/opencontainers/image-spec/specs-go/v1"

// Referrer describes an artifact that references a manifest.
//
// Common referrer types include:
//   - Sigstore signatures (application/vnd.dev.sigstore.bundle.v0.3+json)
//   - In-toto attestations (application/vnd.in-toto+json)
//   - SLSA provenance documents
type Referrer struct {
	// Digest is the content-addressable identifier (e.g., "sha256:abc123...").
	Digest string

	// Size is the size of the referrer content in bytes.
	Size int64

	// MediaType identifies the format of the referrer content.
	MediaType string

	// ArtifactType identifies the type of artifact (e.g., signature, attestation).
	ArtifactType string

	// Annotations contains optional metadata key-value pairs.
	Annotations map[string]string
}

// referrerFromDescriptor converts an OCI descriptor to a Referrer.
func referrerFromDescriptor(desc *ocispec.Descriptor) Referrer {
	return Referrer{
		Digest:       desc.Digest.String(),
		Size:         desc.Size,
		MediaType:    desc.MediaType,
		ArtifactType: desc.ArtifactType,
		Annotations:  desc.Annotations,
	}
}
