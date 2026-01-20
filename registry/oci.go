package registry

import (
	"context"
	"io"
	"net/http"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// OCIClient defines the interface for OCI registry operations.
//
// This interface abstracts the low-level OCI operations, allowing different
// implementations (e.g., ORAS-based, mock for testing).
//
//go:generate go run github.com/matryer/moq@latest -out mocks/oci.go -pkg mocks . OCIClient
type OCIClient interface {
	// PushBlob pushes a blob to the repository.
	// The descriptor must contain the pre-computed digest and size.
	PushBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error

	// FetchBlob fetches a blob from the repository.
	// The caller is responsible for closing the returned reader.
	FetchBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error)

	// PushManifest pushes a manifest to the repository with the given tag.
	PushManifest(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error)

	// FetchManifest fetches a manifest from the repository by descriptor.
	FetchManifest(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, []byte, error)

	// Resolve resolves a reference (tag or digest) to a descriptor.
	Resolve(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error)

	// Tag creates or updates a tag pointing to the given descriptor.
	Tag(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error

	// PushManifestByDigest pushes a manifest without a tag, referenced only by digest.
	// This is used for OCI 1.1 referrer artifacts that don't need a tag.
	PushManifestByDigest(ctx context.Context, repoRef string, manifest *ocispec.Manifest) (ocispec.Descriptor, error)

	// BlobURL returns the URL for direct blob access via HTTP range requests.
	BlobURL(repoRef, digest string) (string, error)

	// AuthHeaders returns HTTP headers with authentication for direct blob access.
	AuthHeaders(ctx context.Context, repoRef string) (http.Header, error)

	// InvalidateAuthHeaders clears cached auth headers for the repository host.
	InvalidateAuthHeaders(repoRef string) error
}
