package client

import (
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry"
)

// clientRef holds parsed reference information.
type clientRef struct {
	registry   string
	repository string
	reference  string // tag or digest
}

// parseClientRef parses a reference string into its components.
func parseClientRef(ref string) (clientRef, error) {
	r, err := registry.ParseReference(ref)
	if err != nil {
		return clientRef{}, ErrInvalidReference
	}
	return clientRef{
		registry:   r.Registry,
		repository: r.Repository,
		reference:  r.Reference,
	}, nil
}

// isDigest returns true if the reference is a digest (not a tag).
func isDigest(ref string) bool {
	// Digests contain a colon after the algorithm (e.g., "sha256:abc123...")
	return strings.Contains(ref, ":")
}

// descriptorFromDigest creates a minimal descriptor from a digest string.
// The descriptor has size 0 which signals to FetchManifest to accept any size.
func descriptorFromDigest(dgst string) (ocispec.Descriptor, error) {
	d, err := digest.Parse(dgst)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("%w: invalid digest %q", ErrInvalidReference, dgst)
	}
	return ocispec.Descriptor{
		Digest: d,
	}, nil
}
