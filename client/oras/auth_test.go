package oras

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func TestStaticCredentials(t *testing.T) {
	// No t.Parallel() - subtests share store
	store := StaticCredentials("registry.example.com", "user", "pass")
	require.NotNil(t, store)

	ctx := context.Background()

	t.Run("matching registry returns credentials", func(t *testing.T) {
		cred, err := store.Get(ctx, "registry.example.com")
		require.NoError(t, err)
		assert.Equal(t, "user", cred.Username)
		assert.Equal(t, "pass", cred.Password)
		assert.Empty(t, cred.AccessToken)
	})

	t.Run("non-matching registry returns empty", func(t *testing.T) {
		cred, err := store.Get(ctx, "other.example.com")
		require.NoError(t, err)
		assert.Equal(t, auth.EmptyCredential, cred)
	})

	t.Run("Put returns error", func(t *testing.T) {
		err := store.Put(ctx, "registry.example.com", auth.Credential{})
		assert.Error(t, err)
	})

	t.Run("Delete returns error", func(t *testing.T) {
		err := store.Delete(ctx, "registry.example.com")
		assert.Error(t, err)
	})
}

func TestStaticToken(t *testing.T) {
	// No t.Parallel() - subtests share store
	store := StaticToken("registry.example.com", "my-token")
	require.NotNil(t, store)

	ctx := context.Background()

	t.Run("matching registry returns token", func(t *testing.T) {
		cred, err := store.Get(ctx, "registry.example.com")
		require.NoError(t, err)
		assert.Equal(t, "my-token", cred.AccessToken)
		assert.Empty(t, cred.Username)
		assert.Empty(t, cred.Password)
	})

	t.Run("non-matching registry returns empty", func(t *testing.T) {
		cred, err := store.Get(ctx, "other.example.com")
		require.NoError(t, err)
		assert.Equal(t, auth.EmptyCredential, cred)
	})
}

func TestStaticStore_DockerHubFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		storeHost   string
		queryHost   string
		expectMatch bool
	}{
		{
			name:        "docker.io matches docker.io",
			storeHost:   "docker.io",
			queryHost:   "docker.io",
			expectMatch: true,
		},
		{
			name:        "docker.io matches registry-1.docker.io",
			storeHost:   "docker.io",
			queryHost:   "registry-1.docker.io",
			expectMatch: true,
		},
		{
			name:        "registry-1.docker.io matches docker.io",
			storeHost:   "registry-1.docker.io",
			queryHost:   "docker.io",
			expectMatch: true,
		},
		{
			name:        "index.docker.io matches docker.io",
			storeHost:   "index.docker.io",
			queryHost:   "docker.io",
			expectMatch: true,
		},
		{
			name:        "docker.io does not match other registry",
			storeHost:   "docker.io",
			queryHost:   "ghcr.io",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := StaticCredentials(tt.storeHost, "user", "pass")
			cred, err := store.Get(context.Background(), tt.queryHost)
			require.NoError(t, err)

			if tt.expectMatch {
				assert.Equal(t, "user", cred.Username)
			} else {
				assert.Equal(t, auth.EmptyCredential, cred)
			}
		})
	}
}

func TestNormalizeServerAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"registry.example.com", "registry.example.com"},
		{"registry.example.com:5000", "registry.example.com:5000"},
		{"https://registry.example.com", "registry.example.com"},
		{"http://registry.example.com", "registry.example.com"},
		{"https://registry.example.com/v2/", "registry.example.com"},
		{"https://registry.example.com:5000/v2/repo", "registry.example.com:5000"},
		{"http://localhost:5000/v2/", "localhost:5000"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := normalizeServerAddress(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"registry.example.com", "registry.example.com"},
		{"registry.example.com:5000", "registry.example.com"},
		{"localhost:5000", "localhost"},
		{"[::1]:8080", "[::1]"},
		{"[::1]", "[::1]"},
		{"192.168.1.1:5000", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := extractHost(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDockerHubHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host     string
		expected bool
	}{
		{"docker.io", true},
		{"docker.io:443", true},
		{"registry-1.docker.io", true},
		{"registry-1.docker.io:443", true},
		{"index.docker.io", true},
		{"ghcr.io", false},
		{"quay.io", false},
		{"registry.example.com", false},
		{"localhost:5000", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()
			result := isDockerHubHost(tt.host)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsEmptyCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cred     auth.Credential
		expected bool
	}{
		{
			name:     "EmptyCredential constant",
			cred:     auth.EmptyCredential,
			expected: true,
		},
		{
			name:     "zero value",
			cred:     auth.Credential{},
			expected: true,
		},
		{
			name:     "with username only",
			cred:     auth.Credential{Username: "user"},
			expected: false,
		},
		{
			name:     "with password only",
			cred:     auth.Credential{Password: "pass"},
			expected: false,
		},
		{
			name:     "with access token",
			cred:     auth.Credential{AccessToken: "token"},
			expected: false,
		},
		{
			name:     "with refresh token",
			cred:     auth.Credential{RefreshToken: "refresh"},
			expected: false,
		},
		{
			name:     "with username and password",
			cred:     auth.Credential{Username: "user", Password: "pass"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isEmptyCredential(tt.cred)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDockerHubFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("returns fallbacks for docker hub host", func(t *testing.T) {
		t.Parallel()
		fallbacks := dockerHubFallbacks("docker.io")
		require.NotEmpty(t, fallbacks)
		assert.Contains(t, fallbacks, "docker.io")
		assert.Contains(t, fallbacks, "registry-1.docker.io")
		assert.Contains(t, fallbacks, "index.docker.io")
	})

	t.Run("returns nil for non-docker-hub host", func(t *testing.T) {
		t.Parallel()
		fallbacks := dockerHubFallbacks("ghcr.io")
		assert.Nil(t, fallbacks)
	})
}

// mockStore implements credentials.Store for testing dockerHubFallbackStore.
type mockStore struct {
	creds map[string]auth.Credential
}

func (m *mockStore) Get(_ context.Context, serverAddress string) (auth.Credential, error) {
	if cred, ok := m.creds[serverAddress]; ok {
		return cred, nil
	}
	return auth.EmptyCredential, nil
}

func (m *mockStore) Put(_ context.Context, serverAddress string, cred auth.Credential) error {
	m.creds[serverAddress] = cred
	return nil
}

func (m *mockStore) Delete(_ context.Context, serverAddress string) error {
	delete(m.creds, serverAddress)
	return nil
}

func TestDockerHubFallbackStore(t *testing.T) {
	t.Parallel()

	t.Run("returns credential from primary address", func(t *testing.T) {
		t.Parallel()
		inner := &mockStore{
			creds: map[string]auth.Credential{
				"registry.example.com": {Username: "user", Password: "pass"},
			},
		}
		store := &dockerHubFallbackStore{store: inner}

		cred, err := store.Get(context.Background(), "registry.example.com")
		require.NoError(t, err)
		assert.Equal(t, "user", cred.Username)
	})

	t.Run("tries fallback addresses for docker hub", func(t *testing.T) {
		t.Parallel()
		inner := &mockStore{
			creds: map[string]auth.Credential{
				"https://index.docker.io/v1/": {Username: "dockeruser", Password: "dockerpass"},
			},
		}
		store := &dockerHubFallbackStore{store: inner}

		cred, err := store.Get(context.Background(), "docker.io")
		require.NoError(t, err)
		assert.Equal(t, "dockeruser", cred.Username)
	})

	t.Run("returns empty for non-docker-hub without credential", func(t *testing.T) {
		t.Parallel()
		inner := &mockStore{creds: map[string]auth.Credential{}}
		store := &dockerHubFallbackStore{store: inner}

		cred, err := store.Get(context.Background(), "ghcr.io")
		require.NoError(t, err)
		assert.Equal(t, auth.EmptyCredential, cred)
	})

	t.Run("Put delegates to inner store", func(t *testing.T) {
		t.Parallel()
		inner := &mockStore{creds: map[string]auth.Credential{}}
		store := &dockerHubFallbackStore{store: inner}

		err := store.Put(context.Background(), "example.com", auth.Credential{Username: "test"})
		require.NoError(t, err)
		assert.Equal(t, "test", inner.creds["example.com"].Username)
	})

	t.Run("Delete delegates to inner store", func(t *testing.T) {
		t.Parallel()
		inner := &mockStore{
			creds: map[string]auth.Credential{
				"example.com": {Username: "test"},
			},
		}
		store := &dockerHubFallbackStore{store: inner}

		err := store.Delete(context.Background(), "example.com")
		require.NoError(t, err)
		_, ok := inner.creds["example.com"]
		assert.False(t, ok)
	})
}
