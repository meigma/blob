package sigstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/sign"
	"google.golang.org/protobuf/encoding/protojson"
)

// TokenProvider is a function that returns an OIDC token.
// Used for keyless signing with Fulcio.
type TokenProvider func(ctx context.Context) (string, error)

// Signature holds a cryptographic signature and its format metadata.
type Signature struct {
	// Data contains the signature bytes (sigstore bundle JSON).
	Data []byte

	// MediaType indicates the signature format.
	MediaType string
}

// Signer creates Sigstore bundles for signing OCI artifacts.
type Signer struct {
	keypair       sign.Keypair
	opts          sign.BundleOptions
	tokenProvider TokenProvider
}

// NewSigner creates a sigstore-based signer.
func NewSigner(opts ...SignerOption) (*Signer, error) {
	s := &Signer{}
	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	// Require a keypair
	if s.keypair == nil {
		return nil, errors.New("sigstore: no keypair configured (use WithEphemeralKey or WithPrivateKey)")
	}

	return s, nil
}

// Sign creates a signature for the given payload.
// The payload is typically the raw manifest JSON that was pushed.
// Returns a Signature containing the sigstore bundle JSON.
func (s *Signer) Sign(ctx context.Context, payload []byte) (*Signature, error) {
	content := &sign.PlainData{Data: payload}

	opts := s.opts
	opts.Context = ctx

	// Get OIDC token if using Fulcio (keyless signing)
	if s.tokenProvider != nil && opts.CertificateProvider != nil {
		token, err := s.tokenProvider(ctx)
		if err != nil {
			return nil, fmt.Errorf("sigstore get token: %w", err)
		}
		opts.CertificateProviderOptions = &sign.CertificateProviderOptions{
			IDToken: token,
		}
	}

	bundle, err := sign.Bundle(content, s.keypair, opts)
	if err != nil {
		return nil, fmt.Errorf("sigstore sign: %w", err)
	}

	bundleJSON, err := protojson.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("sigstore marshal bundle: %w", err)
	}

	return &Signature{
		Data:      bundleJSON,
		MediaType: SignatureArtifactType,
	}, nil
}
