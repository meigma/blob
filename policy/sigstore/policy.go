package sigstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meigma/blob/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// SignatureArtifactType is the OCI artifact type for sigstore bundles.
const SignatureArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// Policy implements client.Policy using sigstore-go for signature verification.
// It fetches sigstore bundle referrers from the registry and verifies them
// against the trusted root.
type Policy struct {
	trustedRoot root.TrustedMaterial
	identity    *verify.CertificateIdentity
	logger      *slog.Logger
}

// NewPolicy creates a sigstore-based verification policy.
func NewPolicy(opts ...PolicyOption) (*Policy, error) {
	p := &Policy{
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, err
		}
	}

	// Default to public Sigstore instance if no trusted root provided
	if p.trustedRoot == nil {
		tr, err := root.FetchTrustedRoot()
		if err != nil {
			return nil, fmt.Errorf("sigstore fetch trusted root: %w", err)
		}
		p.trustedRoot = tr
	}

	// Warn if no identity is configured
	if p.identity == nil {
		p.logger.Warn("sigstore policy created without identity requirement; " +
			"any valid signature will be accepted regardless of signer")
	}

	return p, nil
}

// Evaluate implements client.Policy.
func (p *Policy) Evaluate(ctx context.Context, req client.PolicyRequest) error {
	// List sigstore bundle referrers for the subject manifest
	referrers, err := req.Client.Referrers(ctx, req.Ref, req.Subject, SignatureArtifactType)
	if err != nil {
		if errors.Is(err, client.ErrReferrersUnsupported) {
			return fmt.Errorf("sigstore: registry does not support referrers API")
		}
		return fmt.Errorf("sigstore: list referrers: %w", err)
	}

	if len(referrers) == 0 {
		return errors.New("sigstore: no signatures found for manifest")
	}

	// Get the manifest payload for verification
	payload, err := req.Client.FetchDescriptor(ctx, req.Ref, req.Subject)
	if err != nil {
		return fmt.Errorf("sigstore: fetch manifest: %w", err)
	}

	// Try to verify at least one signature
	var lastErr error
	for _, ref := range referrers {
		bundleData, err := req.Client.FetchDescriptor(ctx, req.Ref, ref)
		if err != nil {
			lastErr = fmt.Errorf("sigstore: fetch bundle: %w", err)
			continue
		}

		if err := p.verifyBundleData(ctx, req, bundleData, payload); err != nil {
			lastErr = err
			continue
		}

		// Successfully verified
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("sigstore: verification failed: %w", lastErr)
	}
	return errors.New("sigstore: no valid signatures found")
}

type ociArtifactManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType,omitempty"`
	Layers        []ocispec.Descriptor `json:"layers,omitempty"`
	Blobs         []ocispec.Descriptor `json:"blobs,omitempty"`
}

func parseOCIArtifactLayers(data []byte) ([]ocispec.Descriptor, bool, error) {
	var manifest ociArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, false, nil
	}
	if manifest.SchemaVersion != 2 {
		return nil, false, nil
	}

	layers := manifest.Layers
	if len(layers) == 0 {
		layers = manifest.Blobs
	}
	if len(layers) == 0 {
		return nil, true, errors.New("sigstore: manifest contains no layers")
	}

	return layers, true, nil
}

func (p *Policy) verifyBundleData(ctx context.Context, req client.PolicyRequest, data, payload []byte) error {
	layers, ok, err := parseOCIArtifactLayers(data)
	if err != nil {
		return err
	}
	if ok {
		var lastErr error
		for _, layer := range layers {
			layerData, err := req.Client.FetchDescriptor(ctx, req.Ref, layer)
			if err != nil {
				lastErr = fmt.Errorf("sigstore: fetch bundle layer: %w", err)
				continue
			}

			if err := p.verifyBundle(layerData, payload); err != nil {
				lastErr = err
				continue
			}

			return nil
		}

		if lastErr != nil {
			return lastErr
		}
		return errors.New("sigstore: no valid bundle layers found")
	}

	return p.verifyBundle(data, payload)
}

// verifyBundle verifies a sigstore bundle against the payload.
func (p *Policy) verifyBundle(bundleData, payload []byte) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleData); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}

	// Build verifier with transparency log and timestamp requirements
	verifier, err := verify.NewVerifier(
		p.trustedRoot,
		verify.WithObserverTimestamps(1),
		verify.WithTransparencyLog(1),
	)
	if err != nil {
		return fmt.Errorf("create verifier: %w", err)
	}

	// Build verification policy
	var policyOpts []verify.PolicyOption
	if p.identity != nil {
		policyOpts = append(policyOpts, verify.WithCertificateIdentity(*p.identity))
	} else {
		policyOpts = append(policyOpts, verify.WithoutIdentitiesUnsafe())
	}

	policy := verify.NewPolicy(
		verify.WithArtifact(bytes.NewReader(payload)),
		policyOpts...,
	)

	_, err = verifier.Verify(&b, policy)
	if err != nil {
		return fmt.Errorf("signature invalid: %w", err)
	}

	return nil
}

// Ensure Policy implements client.Policy.
var _ client.Policy = (*Policy)(nil)
