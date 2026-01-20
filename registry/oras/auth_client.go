package oras

import (
	"net/http"

	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// authTransport is an http.RoundTripper that handles OCI registry authentication.
// It wraps an auth.Client to automatically add repository scope to requests.
type authTransport struct {
	client *auth.Client
	ref    registry.Reference
}

// RoundTrip implements http.RoundTripper by appending repository pull scope
// to the request context and delegating to the underlying auth client.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := auth.AppendRepositoryScope(req.Context(), t.ref, auth.ActionPull)
	req = req.Clone(ctx)
	return t.client.Do(req)
}

// AuthClient returns an HTTP client that handles registry auth, including token exchange.
func (c *Client) AuthClient(repoRef string) (*http.Client, error) {
	ref, err := parseRef(repoRef)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: &authTransport{
			client: c.authClient,
			ref:    ref,
		},
	}, nil
}
