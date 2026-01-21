package registry

import (
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Policy evaluates whether a manifest is trusted.
type Policy interface {
	Evaluate(ctx context.Context, req PolicyRequest) error
}

// PolicyFunc is an adapter to allow ordinary functions as policies.
type PolicyFunc func(ctx context.Context, req PolicyRequest) error

// Evaluate calls f(ctx, req).
//
//nolint:gocritic // matches Policy interface signature
func (f PolicyFunc) Evaluate(ctx context.Context, req PolicyRequest) error {
	return f(ctx, req)
}

// PolicyRequest provides context for policy evaluation.
type PolicyRequest struct {
	Ref      string
	Digest   string
	Manifest *BlobManifest
	Subject  ocispec.Descriptor
	Client   PolicyClient
}

// PolicyClient exposes minimal client capabilities for policies.
type PolicyClient interface {
	Referrers(ctx context.Context, ref string, subject ocispec.Descriptor, artifactType string) ([]ocispec.Descriptor, error)
	FetchDescriptor(ctx context.Context, ref string, desc ocispec.Descriptor) ([]byte, error)
}

func (c *Client) evaluatePolicies(ctx context.Context, ref, digestStr string, manifest *BlobManifest, raw []byte) error {
	if len(c.policies) == 0 {
		return nil
	}

	c.log().Debug("evaluating policies", "ref", ref, "policy_count", len(c.policies))

	dgst, err := digest.Parse(digestStr)
	if err != nil {
		return fmt.Errorf("%w: invalid digest %q", ErrInvalidReference, digestStr)
	}

	subject := ocispec.Descriptor{
		MediaType: manifest.Raw().MediaType,
		Digest:    dgst,
	}
	if raw != nil {
		subject.Size = int64(len(raw))
	}

	req := PolicyRequest{
		Ref:      ref,
		Digest:   digestStr,
		Manifest: manifest,
		Subject:  subject,
		Client:   c,
	}

	for i, policy := range c.policies {
		if err := policy.Evaluate(ctx, req); err != nil {
			c.log().Warn("policy evaluation failed",
				"policy_index", i,
				"error", err.Error(),
			)
			return fmt.Errorf("%w: %v", ErrPolicyViolation, err)
		}
	}

	return nil
}
