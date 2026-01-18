package client

import (
	"context"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type referrersProvider interface {
	Referrers(ctx context.Context, repoRef string, subject ocispec.Descriptor, artifactType string) ([]ocispec.Descriptor, error)
}

// Referrers lists referrer descriptors for the given subject manifest.
func (c *Client) Referrers(ctx context.Context, ref string, subject ocispec.Descriptor, artifactType string) ([]ocispec.Descriptor, error) {
	provider, ok := c.oci.(referrersProvider)
	if !ok {
		return nil, ErrReferrersUnsupported
	}
	referrers, err := provider.Referrers(ctx, ref, subject, artifactType)
	if err != nil {
		return nil, mapOCIError(err)
	}
	return referrers, nil
}

// FetchDescriptor fetches raw content for the given descriptor.
func (c *Client) FetchDescriptor(ctx context.Context, ref string, desc ocispec.Descriptor) ([]byte, error) {
	reader, err := c.oci.FetchBlob(ctx, ref, &desc)
	if err != nil {
		return nil, mapOCIError(err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read descriptor: %w", err)
	}
	return data, nil
}
