// Package oci provides a generic OCI client layer wrapping ORAS.
//
// Client provides blob-agnostic operations for interacting with OCI registries,
// handling authentication and OCI 1.0/1.1 compatibility transparently.
package oci

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/errcode"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Client provides generic OCI registry operations.
//
// It wraps ORAS to provide a simplified interface for pushing and pulling
// blobs and manifests. OCI 1.0/1.1 compatibility is handled transparently.
type Client struct {
	plainHTTP       bool
	userAgent       string
	anonymous       bool // skip credential lookup entirely
	credStore       credentials.Store
	authClient      *auth.Client // shared auth client with token cache
	authHeaderCache *authHeaderCache
}

// New creates a new OCI client with the given options.
func New(opts ...Option) *Client {
	c := &Client{
		userAgent:       "blob-client/1.0",
		authHeaderCache: newAuthHeaderCache(defaultAuthHeaderCacheTTL),
	}
	for _, opt := range opts {
		opt(c)
	}

	// Build shared auth client with token cache
	c.authClient = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: func(ctx context.Context, hostport string) (auth.Credential, error) {
			if c.anonymous || c.credStore == nil {
				return auth.EmptyCredential, nil
			}
			return c.credStore.Get(ctx, hostport)
		},
		Header: http.Header{
			"User-Agent": []string{c.userAgent},
		},
	}

	return c
}

// repository creates a Repository for the given reference.
// Uses the shared auth client to reuse tokens across requests.
func (c *Client) repository(ref string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("parse reference %q: %w", ref, err)
	}

	repo.PlainHTTP = c.plainHTTP
	repo.Client = c.authClient

	return repo, nil
}

// parseRef parses a full reference into registry, repository, and tag/digest.
func parseRef(ref string) (registry.Reference, error) {
	r, err := registry.ParseReference(ref)
	if err != nil {
		return registry.Reference{}, fmt.Errorf("%w: %v", ErrInvalidReference, err)
	}
	return r, nil
}

// PushBlob pushes a blob to the repository.
//
// The descriptor must contain the pre-computed digest and size.
// The blob content is read from r, which must provide exactly desc.Size bytes.
// This allows streaming large blobs without loading them into memory.
func (c *Client) PushBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor, r io.Reader) error {
	if err := validateDescriptor(desc); err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("%w: content reader is nil", ErrInvalidDescriptor)
	}

	repo, err := c.repository(repoRef)
	if err != nil {
		return err
	}

	if err := repo.Push(ctx, *desc, r); err != nil {
		return mapError(err)
	}

	return nil
}

// FetchBlob fetches a blob from the repository using the provided descriptor.
//
// The descriptor must contain the digest and size (typically from a manifest).
// The caller is responsible for closing the returned reader.
func (c *Client) FetchBlob(ctx context.Context, repoRef string, desc *ocispec.Descriptor) (io.ReadCloser, error) {
	if err := validateDescriptor(desc); err != nil {
		return nil, err
	}

	repo, err := c.repository(repoRef)
	if err != nil {
		return nil, err
	}

	rc, err := repo.Fetch(ctx, *desc)
	if err != nil {
		return nil, mapError(err)
	}

	return rc, nil
}

// PushManifest pushes a manifest to the repository.
//
// Handles OCI 1.0/1.1 compatibility transparently: uses OCI image manifest format
// which works with both 1.0 and 1.1 registries.
func (c *Client) PushManifest(ctx context.Context, repoRef, tag string, manifest *ocispec.Manifest) (ocispec.Descriptor, error) {
	if manifest == nil {
		return ocispec.Descriptor{}, fmt.Errorf("%w: manifest is nil", ErrManifestInvalid)
	}
	repo, err := c.repository(repoRef)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// Serialize manifest
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("marshal manifest: %w", err)
	}

	dgst := digest.FromBytes(manifestJSON)
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    dgst,
		Size:      int64(len(manifestJSON)),
	}

	// Push with tag reference
	if err := repo.PushReference(ctx, desc, bytes.NewReader(manifestJSON), tag); err != nil {
		return ocispec.Descriptor{}, mapError(err)
	}

	return desc, nil
}

// FetchManifest fetches a manifest from the repository by descriptor.
//
// Call Resolve first and pass the resolved descriptor to avoid extra lookups.
// Handles both OCI 1.0 and 1.1 manifest formats.
func (c *Client) FetchManifest(ctx context.Context, repoRef string, expected *ocispec.Descriptor) (ocispec.Manifest, error) {
	if err := validateDescriptor(expected); err != nil {
		return ocispec.Manifest{}, err
	}
	if expected.MediaType != "" && expected.MediaType != ocispec.MediaTypeImageManifest {
		return ocispec.Manifest{}, fmt.Errorf("%w: unsupported media type %s", ErrManifestInvalid, expected.MediaType)
	}

	repo, err := c.repository(repoRef)
	if err != nil {
		return ocispec.Manifest{}, err
	}

	desc, rc, err := repo.FetchReference(ctx, expected.Digest.String())
	if err != nil {
		return ocispec.Manifest{}, mapError(err)
	}
	defer rc.Close()

	if expected.MediaType == "" && desc.MediaType != "" && desc.MediaType != ocispec.MediaTypeImageManifest {
		return ocispec.Manifest{}, fmt.Errorf("%w: unsupported media type %s", ErrManifestInvalid, desc.MediaType)
	}

	limited := io.LimitReader(rc, expected.Size)

	var manifest ocispec.Manifest
	if err := json.NewDecoder(limited).Decode(&manifest); err != nil {
		return ocispec.Manifest{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}

	return manifest, nil
}

// Resolve resolves a reference to a descriptor.
//
// The ref can be a tag or digest.
func (c *Client) Resolve(ctx context.Context, repoRef, ref string) (ocispec.Descriptor, error) {
	repo, err := c.repository(repoRef)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	desc, err := repo.Resolve(ctx, ref)
	if err != nil {
		return ocispec.Descriptor{}, mapError(err)
	}

	return desc, nil
}

// Tag creates or updates a tag pointing to the given digest.
func (c *Client) Tag(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
	if err := validateDescriptor(desc); err != nil {
		return err
	}

	repo, err := c.repository(repoRef)
	if err != nil {
		return err
	}

	if err := repo.Tag(ctx, *desc, tag); err != nil {
		return mapError(err)
	}

	return nil
}

// BlobURL returns the URL for direct blob access.
//
// This is used for lazy blob access via HTTP range requests.
func (c *Client) BlobURL(repoRef, dgst string) (string, error) {
	ref, err := parseRef(repoRef)
	if err != nil {
		return "", err
	}

	scheme := "https"
	if c.plainHTTP {
		scheme = "http"
	}

	// Build URL: <scheme>://<registry>/v2/<repository>/blobs/<digest>
	url := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", scheme, ref.Host(), ref.Repository, dgst)
	return url, nil
}

// AuthHeaders returns HTTP headers with authentication for direct blob access.
//
// This is used for lazy blob access via HTTP range requests. Returns raw
// credentials (basic auth or static bearer token) from the credential store.
// Note: This does not perform OAuth2 token exchange. For registries that
// require token exchange, use FetchBlob or the registry's token endpoint.
// If a request returns 401, call InvalidateAuthHeaders and retry to refresh.
func (c *Client) AuthHeaders(ctx context.Context, repoRef string) (http.Header, error) {
	ref, err := parseRef(repoRef)
	if err != nil {
		return nil, err
	}
	host := ref.Host()

	headers := make(http.Header)
	headers.Set("User-Agent", c.userAgent)

	if c.anonymous || c.credStore == nil {
		return headers, nil
	}

	if c.authHeaderCache != nil {
		if authValue, ok := c.authHeaderCache.get(host); ok {
			if authValue != "" {
				headers.Set("Authorization", authValue)
			}
			return headers, nil
		}
	}

	cred, err := c.credStore.Get(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("get credentials for %s: %w", host, err)
	}

	if isEmptyCredential(cred) {
		return headers, nil
	}

	var authValue string
	// If we have an access token, use bearer auth
	if cred.AccessToken != "" {
		authValue = "Bearer " + cred.AccessToken
	}

	// If we have username/password, use basic auth
	if authValue == "" && cred.Username != "" {
		authValue = basicAuth(cred.Username, cred.Password)
	}

	if authValue != "" {
		headers.Set("Authorization", authValue)
		if c.authHeaderCache != nil {
			c.authHeaderCache.set(host, authValue)
		}
	}

	return headers, nil
}

// InvalidateAuthHeaders clears cached auth headers for the repository host.
// Call this after receiving a 401 to force the next AuthHeaders call to refresh.
func (c *Client) InvalidateAuthHeaders(repoRef string) error {
	if c.authHeaderCache == nil {
		return nil
	}
	ref, err := parseRef(repoRef)
	if err != nil {
		return err
	}
	c.authHeaderCache.invalidate(ref.Host())
	return nil
}

// basicAuth returns the basic auth header value.
func basicAuth(username, password string) string {
	creds := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// validateDescriptor checks that a descriptor is valid for use.
func validateDescriptor(desc *ocispec.Descriptor) error {
	if desc == nil {
		return fmt.Errorf("%w: descriptor is nil", ErrInvalidDescriptor)
	}
	if desc.Size < 0 {
		return fmt.Errorf("%w: negative size %d", ErrInvalidDescriptor, desc.Size)
	}
	if desc.Digest == "" {
		return fmt.Errorf("%w: empty digest", ErrInvalidDescriptor)
	}
	if err := desc.Digest.Validate(); err != nil {
		return fmt.Errorf("%w: invalid digest %q: %v", ErrInvalidDescriptor, desc.Digest, err)
	}
	return nil
}

// mapError maps ORAS errors to our sentinel errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdef.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	// ORAS wraps HTTP errors, check for specific error types
	var errResp *errcode.ErrorResponse
	if errors.As(err, &errResp) {
		switch errResp.StatusCode {
		case http.StatusNotFound:
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		case http.StatusUnauthorized:
			return fmt.Errorf("%w: %v", ErrUnauthorized, err)
		case http.StatusForbidden:
			return fmt.Errorf("%w: %v", ErrForbidden, err)
		}
	}
	return err
}
