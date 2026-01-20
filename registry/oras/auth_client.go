package oras

import (
	"net/http"

	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote/auth"
)

type authTransport struct {
	client *auth.Client
	ref    registry.Reference
}

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
