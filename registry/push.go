package registry

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	blob "github.com/meigma/blob/core"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Push pushes a blob archive to an OCI registry.
//
// The archive is pushed as two blobs (index and data) with a manifest
// linking them. The ref must include a tag (e.g., "registry.com/repo:v1.0.0").
//
// Use WithTags to apply additional tags to the same manifest.
func (c *Client) Push(ctx context.Context, ref string, b *blob.Blob, opts ...PushOption) error {
	cfg := pushConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Parse reference to extract tag
	parsedRef, err := parseClientRef(ref)
	if err != nil {
		return err
	}
	tag := parsedRef.reference
	if tag == "" || isDigest(tag) {
		return fmt.Errorf("%w: reference must include a tag", ErrInvalidReference)
	}

	// Step 1: Push empty config blob (required by OCI spec)
	configDesc, err := c.pushEmptyConfig(ctx, ref)
	if err != nil {
		return fmt.Errorf("push config: %w", err)
	}

	// Step 2: Push index blob
	indexData := b.IndexData()
	indexDesc := ocispec.Descriptor{
		MediaType: MediaTypeIndex,
		Digest:    digest.FromBytes(indexData),
		Size:      int64(len(indexData)),
	}
	if pushErr := c.oci.PushBlob(ctx, ref, &indexDesc, bytes.NewReader(indexData)); pushErr != nil {
		return fmt.Errorf("push index blob: %w", mapOCIError(pushErr))
	}

	// Step 3: Build data descriptor from pre-computed metadata in index
	dataDesc, err := dataDescriptor(b)
	if err != nil {
		return err
	}

	// Step 4: Push data blob
	if pushErr := c.oci.PushBlob(ctx, ref, &dataDesc, b.Stream()); pushErr != nil {
		return fmt.Errorf("push data blob: %w", mapOCIError(pushErr))
	}

	// Step 5: Build and push manifest
	manifest := buildManifest(&configDesc, &indexDesc, &dataDesc, cfg.annotations)
	manifestDesc, err := c.oci.PushManifest(ctx, ref, tag, &manifest)
	if err != nil {
		return fmt.Errorf("push manifest: %w", mapOCIError(err))
	}

	// Step 6: Apply additional tags
	for _, additionalTag := range cfg.tags {
		if tagErr := c.oci.Tag(ctx, ref, &manifestDesc, additionalTag); tagErr != nil {
			return fmt.Errorf("tag %q: %w", additionalTag, mapOCIError(tagErr))
		}
	}

	return nil
}

// pushEmptyConfig pushes the empty JSON config blob required by OCI manifests.
func (c *Client) pushEmptyConfig(ctx context.Context, ref string) (ocispec.Descriptor, error) {
	config := []byte("{}")
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeEmptyJSON,
		Digest:    digest.FromBytes(config),
		Size:      int64(len(config)),
	}
	if err := c.oci.PushBlob(ctx, ref, &desc, bytes.NewReader(config)); err != nil {
		return ocispec.Descriptor{}, mapOCIError(err)
	}
	return desc, nil
}

// dataDescriptor builds the data blob descriptor from pre-computed metadata.
func dataDescriptor(b *blob.Blob) (ocispec.Descriptor, error) {
	hashBytes, ok := b.DataHash()
	if !ok {
		return ocispec.Descriptor{}, errors.New("push: archive missing data hash in index")
	}
	dataSize, ok := b.DataSize()
	if !ok {
		return ocispec.Descriptor{}, errors.New("push: archive missing data size in index")
	}
	if dataSize > math.MaxInt64 {
		return ocispec.Descriptor{}, errors.New("push: data size exceeds maximum int64")
	}

	// Convert raw SHA256 bytes to OCI digest format (sha256:<hex>)
	dataDigest := digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(hashBytes))
	if err := dataDigest.Validate(); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("push: invalid data hash: %w", err)
	}

	return ocispec.Descriptor{
		MediaType: MediaTypeData,
		Digest:    dataDigest,
		Size:      int64(dataSize), //nolint:gosec // overflow checked above
	}, nil
}

// buildManifest creates an OCI manifest for a blob archive.
func buildManifest(configDesc, indexDesc, dataDesc *ocispec.Descriptor, customAnnotations map[string]string) ocispec.Manifest {
	annotations := make(map[string]string)
	for k, v := range customAnnotations {
		annotations[k] = v
	}
	if _, ok := annotations[ocispec.AnnotationCreated]; !ok {
		annotations[ocispec.AnnotationCreated] = time.Now().UTC().Format(time.RFC3339)
	}

	return ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: ArtifactType,
		Config:       *configDesc,
		Layers:       []ocispec.Descriptor{*indexDesc, *dataDesc},
		Annotations:  annotations,
	}
}
