package oras

import (
	"context"
	"errors"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// DefaultCredentialStore returns a credential store that reads from
// Docker config (~/.docker/config.json) and credential helpers.
func DefaultCredentialStore() (credentials.Store, error) {
	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, err
	}
	return &dockerHubFallbackStore{store: store}, nil
}

// StaticCredentials returns a credential store with a single static credential
// for the specified registry.
func StaticCredentials(registry, username, password string) credentials.Store {
	return &staticStore{
		registry: normalizeServerAddress(registry),
		cred: auth.Credential{
			Username: username,
			Password: password,
		},
	}
}

// StaticToken returns a credential store with a bearer token
// for the specified registry.
func StaticToken(registry, token string) credentials.Store {
	return &staticStore{
		registry: normalizeServerAddress(registry),
		cred: auth.Credential{
			AccessToken: token,
		},
	}
}

// staticStore implements credentials.Store for a single static credential.
type staticStore struct {
	registry string
	cred     auth.Credential
}

// Get retrieves credentials for the given server address.
func (s *staticStore) Get(_ context.Context, serverAddress string) (auth.Credential, error) {
	server := normalizeServerAddress(serverAddress)
	if server == s.registry {
		return s.cred, nil
	}
	if isDockerHubHost(server) && isDockerHubHost(s.registry) {
		return s.cred, nil
	}
	return auth.EmptyCredential, nil
}

// Put is not supported for static credentials.
func (s *staticStore) Put(_ context.Context, _ string, _ auth.Credential) error {
	return errors.New("static credential store is read-only")
}

// Delete is not supported for static credentials.
func (s *staticStore) Delete(_ context.Context, _ string) error {
	return errors.New("static credential store is read-only")
}

// dockerHubFallbackStore wraps a credential store and tries multiple
// Docker Hub hostnames when looking up credentials.
type dockerHubFallbackStore struct {
	store credentials.Store
}

// Get retrieves credentials, trying Docker Hub fallback addresses if needed.
func (s *dockerHubFallbackStore) Get(ctx context.Context, serverAddress string) (auth.Credential, error) {
	cred, err := s.store.Get(ctx, serverAddress)
	if err == nil && !isEmptyCredential(cred) {
		return cred, nil
	}

	// Try Docker Hub fallback addresses.
	for _, alt := range dockerHubFallbacks(serverAddress) {
		if alt == serverAddress {
			continue
		}
		fallbackCred, fallbackErr := s.store.Get(ctx, alt)
		if fallbackErr == nil && !isEmptyCredential(fallbackCred) {
			return fallbackCred, nil
		}
	}

	// If the original lookup returned an error and no fallback succeeded,
	// return the original error. Otherwise return the (possibly empty) credential.
	if err != nil {
		return cred, err
	}
	return cred, nil
}

// Put saves credentials to the underlying store.
func (s *dockerHubFallbackStore) Put(ctx context.Context, serverAddress string, cred auth.Credential) error {
	return s.store.Put(ctx, serverAddress, cred)
}

// Delete removes credentials from the underlying store.
func (s *dockerHubFallbackStore) Delete(ctx context.Context, serverAddress string) error {
	return s.store.Delete(ctx, serverAddress)
}

// dockerHubFallbacks returns alternative addresses to try for Docker Hub.
func dockerHubFallbacks(serverAddress string) []string {
	hostport := normalizeServerAddress(serverAddress)
	if !isDockerHubHost(hostport) {
		return nil
	}
	return []string{
		"https://index.docker.io/v1/",
		"index.docker.io",
		"registry-1.docker.io",
		"docker.io",
	}
}

// isDockerHubHost returns true if the address is a Docker Hub hostname.
// Handles addresses with or without ports (e.g., "docker.io" or "docker.io:443").
func isDockerHubHost(hostport string) bool {
	host := extractHost(hostport)
	switch host {
	case "docker.io", "registry-1.docker.io", "index.docker.io":
		return true
	default:
		return false
	}
}

// extractHost returns just the hostname from a host[:port] string.
func extractHost(hostport string) string {
	// Handle IPv6 addresses like [::1]:8080
	if strings.HasPrefix(hostport, "[") {
		if idx := strings.LastIndex(hostport, "]"); idx != -1 {
			return hostport[:idx+1]
		}
		return hostport
	}
	// Regular host:port
	if idx := strings.LastIndex(hostport, ":"); idx != -1 {
		return hostport[:idx]
	}
	return hostport
}

// normalizeServerAddress extracts the host[:port] from a server address.
// It strips the scheme and path but preserves the port for credential matching.
func normalizeServerAddress(addr string) string {
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	addr, _, _ = strings.Cut(addr, "/")
	return addr
}

// isEmptyCredential returns true if the credential has no authentication data.
func isEmptyCredential(cred auth.Credential) bool {
	return cred == auth.EmptyCredential ||
		(cred.Username == "" && cred.Password == "" && cred.AccessToken == "" && cred.RefreshToken == "")
}
