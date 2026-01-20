package registry

import (
	"bytes"
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// ManifestSigner signs OCI manifest payloads.
type ManifestSigner interface {
	SignManifest(ctx context.Context, payload []byte) (data []byte, mediaType string, err error)
}

// Sign creates a signature for a manifest and attaches it as a referrer.
//
// The ref must include a tag or digest. The signer creates the signature bundle,
// which is pushed as an OCI 1.1 referrer artifact linked to the manifest.
//
// Returns the digest of the signature manifest.
func (c *Client) Sign(ctx context.Context, ref string, signer ManifestSigner, opts ...SignOption) (string, error) {
	cfg := signConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Parse and validate reference
	parsedRef, err := parseClientRef(ref)
	if err != nil {
		return "", err
	}
	if parsedRef.reference == "" {
		return "", fmt.Errorf("%w: reference must include a tag or digest", ErrInvalidReference)
	}

	// Step 1: Resolve to digest
	digestStr, err := c.resolveDigest(ctx, ref, parsedRef.reference, false)
	if err != nil {
		return "", fmt.Errorf("resolve manifest: %w", err)
	}

	// Step 2: Fetch the raw manifest bytes (payload to sign)
	desc, err := descriptorFromDigest(digestStr)
	if err != nil {
		return "", err
	}
	_, raw, err := c.oci.FetchManifest(ctx, ref, &desc)
	if err != nil {
		return "", fmt.Errorf("fetch manifest: %w", mapOCIError(err))
	}

	// Step 3: Sign the manifest
	sigData, sigMediaType, err := signer.SignManifest(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("sign manifest: %w", err)
	}

	// Step 4: Push signature blob
	sigDigest := digest.FromBytes(sigData)
	sigDesc := ocispec.Descriptor{
		MediaType: sigMediaType,
		Digest:    sigDigest,
		Size:      int64(len(sigData)),
	}
	if pushErr := c.oci.PushBlob(ctx, ref, &sigDesc, bytes.NewReader(sigData)); pushErr != nil {
		return "", fmt.Errorf("push signature blob: %w", mapOCIError(pushErr))
	}

	// Step 5: Push empty config blob (required by OCI artifact pattern)
	configDesc, err := c.pushEmptyConfig(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("push config: %w", err)
	}

	// Step 6: Build referrer manifest with Subject pointing to signed manifest
	subject := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    desc.Digest,
		Size:      int64(len(raw)),
	}
	referrerManifest := ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: sigMediaType,
		Config:       configDesc,
		Layers:       []ocispec.Descriptor{sigDesc},
		Subject:      &subject,
	}

	// Step 7: Push referrer manifest by digest (no tag)
	referrerDesc, err := c.oci.PushManifestByDigest(ctx, ref, &referrerManifest)
	if err != nil {
		return "", fmt.Errorf("push referrer manifest: %w", mapOCIError(err))
	}

	return referrerDesc.Digest.String(), nil
}
