package oci

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates client with defaults", func(t *testing.T) {
		t.Parallel()
		c := New()
		require.NotNil(t, c)
		assert.Equal(t, "blob-client/1.0", c.userAgent)
		assert.False(t, c.plainHTTP)
		assert.Nil(t, c.credStore)
		assert.NotNil(t, c.authHeaderCache)
	})

	t.Run("applies WithPlainHTTP option", func(t *testing.T) {
		t.Parallel()
		c := New(WithPlainHTTP(true))
		assert.True(t, c.plainHTTP)
	})

	t.Run("applies WithUserAgent option", func(t *testing.T) {
		t.Parallel()
		c := New(WithUserAgent("custom-agent/2.0"))
		assert.Equal(t, "custom-agent/2.0", c.userAgent)
	})

	t.Run("applies WithStaticCredentials option", func(t *testing.T) {
		t.Parallel()
		c := New(WithStaticCredentials("example.com", "user", "pass"))
		require.NotNil(t, c.credStore)

		cred, err := c.credStore.Get(context.Background(), "example.com")
		require.NoError(t, err)
		assert.Equal(t, "user", cred.Username)
	})

	t.Run("applies WithStaticToken option", func(t *testing.T) {
		t.Parallel()
		c := New(WithStaticToken("example.com", "my-token"))
		require.NotNil(t, c.credStore)

		cred, err := c.credStore.Get(context.Background(), "example.com")
		require.NoError(t, err)
		assert.Equal(t, "my-token", cred.AccessToken)
	})

	t.Run("applies WithAuthHeaderCacheTTL option", func(t *testing.T) {
		t.Parallel()
		c := New(WithAuthHeaderCacheTTL(5 * time.Minute))
		require.NotNil(t, c.authHeaderCache)
		assert.Equal(t, 5*time.Minute, c.authHeaderCache.ttl)
	})

	t.Run("disables auth cache with zero TTL", func(t *testing.T) {
		t.Parallel()
		c := New(WithAuthHeaderCacheTTL(0))
		assert.Nil(t, c.authHeaderCache)
	})

	t.Run("applies WithAnonymous option", func(t *testing.T) {
		t.Parallel()
		c := New(WithAnonymous())
		assert.True(t, c.anonymous)
	})

	t.Run("WithAnonymous skips credential store", func(t *testing.T) {
		t.Parallel()
		store := &countingStore{
			inner: StaticCredentials("registry.example.com", "user", "pass"),
		}
		c := New(
			WithCredentialStore(store),
			WithAnonymous(),
		)

		headers, err := c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)

		// Should not have hit the store
		assert.Equal(t, int32(0), store.getCount.Load())
		// Should not have auth header
		assert.Empty(t, headers.Get("Authorization"))
	})
}

func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ref         string
		wantHost    string
		wantRepo    string
		wantRef     string
		expectError bool
	}{
		{
			name:     "full reference with tag",
			ref:      "registry.example.com/myrepo:v1.0.0",
			wantHost: "registry.example.com",
			wantRepo: "myrepo",
			wantRef:  "v1.0.0",
		},
		{
			name:     "full reference with digest",
			ref:      "registry.example.com/myrepo@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantHost: "registry.example.com",
			wantRepo: "myrepo",
			wantRef:  "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "docker hub with explicit registry",
			ref:      "docker.io/library/alpine:latest",
			wantHost: "registry-1.docker.io", // ORAS normalizes docker.io
			wantRepo: "library/alpine",
			wantRef:  "latest",
		},
		{
			name:     "registry with port",
			ref:      "localhost:5000/myrepo:tag",
			wantHost: "localhost:5000",
			wantRepo: "myrepo",
			wantRef:  "tag",
		},
		{
			name:     "nested repository path",
			ref:      "ghcr.io/owner/repo/image:v1",
			wantHost: "ghcr.io",
			wantRepo: "owner/repo/image",
			wantRef:  "v1",
		},
		{
			name:        "empty reference",
			ref:         "",
			expectError: true,
		},
		{
			name:        "invalid reference",
			ref:         ":::invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref, err := parseRef(tt.ref)

			if tt.expectError {
				assert.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidReference)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, ref.Host())
			assert.Equal(t, tt.wantRepo, ref.Repository)
			assert.Equal(t, tt.wantRef, ref.Reference)
		})
	}
}

func TestValidateDescriptor(t *testing.T) {
	t.Parallel()

	validDigest := digest.FromString("test content")

	tests := []struct {
		name        string
		desc        *ocispec.Descriptor
		expectError bool
		errorIs     error
	}{
		{
			name:        "nil descriptor",
			desc:        nil,
			expectError: true,
			errorIs:     ErrInvalidDescriptor,
		},
		{
			name: "negative size",
			desc: &ocispec.Descriptor{
				Digest: validDigest,
				Size:   -1,
			},
			expectError: true,
			errorIs:     ErrInvalidDescriptor,
		},
		{
			name: "empty digest",
			desc: &ocispec.Descriptor{
				Size: 100,
			},
			expectError: true,
			errorIs:     ErrInvalidDescriptor,
		},
		{
			name: "invalid digest format",
			desc: &ocispec.Descriptor{
				Digest: "not-a-valid-digest",
				Size:   100,
			},
			expectError: true,
			errorIs:     ErrInvalidDescriptor,
		},
		{
			name: "valid descriptor",
			desc: &ocispec.Descriptor{
				Digest: validDigest,
				Size:   100,
			},
			expectError: false,
		},
		{
			name: "valid descriptor with zero size",
			desc: &ocispec.Descriptor{
				Digest: validDigest,
				Size:   0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateDescriptor(tt.desc)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorIs != nil {
					assert.ErrorIs(t, err, tt.errorIs)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBlobURL(t *testing.T) {
	t.Parallel()

	dgst := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	tests := []struct {
		name      string
		plainHTTP bool
		repoRef   string
		digest    string
		wantURL   string
		wantError bool
	}{
		{
			name:      "https URL",
			plainHTTP: false,
			repoRef:   "registry.example.com/myrepo:v1",
			digest:    dgst,
			wantURL:   "https://registry.example.com/v2/myrepo/blobs/" + dgst,
		},
		{
			name:      "http URL with plainHTTP",
			plainHTTP: true,
			repoRef:   "localhost:5000/myrepo:v1",
			digest:    dgst,
			wantURL:   "http://localhost:5000/v2/myrepo/blobs/" + dgst,
		},
		{
			name:      "nested repository",
			plainHTTP: false,
			repoRef:   "ghcr.io/owner/repo/image:tag",
			digest:    dgst,
			wantURL:   "https://ghcr.io/v2/owner/repo/image/blobs/" + dgst,
		},
		{
			name:      "invalid reference",
			plainHTTP: false,
			repoRef:   ":::invalid",
			digest:    dgst,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := New(WithPlainHTTP(tt.plainHTTP))
			url, err := c.BlobURL(tt.repoRef, tt.digest)

			if tt.wantError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, url)
		})
	}
}

func TestAuthHeaders(t *testing.T) {
	t.Parallel()

	t.Run("returns user agent without credentials", func(t *testing.T) {
		t.Parallel()
		c := New(WithUserAgent("test-agent/1.0"))

		headers, err := c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)
		assert.Equal(t, "test-agent/1.0", headers.Get("User-Agent"))
		assert.Empty(t, headers.Get("Authorization"))
	})

	t.Run("returns basic auth header with credentials", func(t *testing.T) {
		t.Parallel()
		c := New(WithStaticCredentials("registry.example.com", "user", "pass"))

		headers, err := c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)

		authHeader := headers.Get("Authorization")
		assert.NotEmpty(t, authHeader)
		assert.Contains(t, authHeader, "Basic ")
	})

	t.Run("returns bearer token header", func(t *testing.T) {
		t.Parallel()
		c := New(WithStaticToken("registry.example.com", "my-secret-token"))

		headers, err := c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)
		assert.Equal(t, "Bearer my-secret-token", headers.Get("Authorization"))
	})

	t.Run("caches auth headers", func(t *testing.T) {
		t.Parallel()
		store := &countingStore{
			inner: StaticCredentials("registry.example.com", "user", "pass"),
		}
		c := New(
			WithCredentialStore(store),
			WithAuthHeaderCacheTTL(time.Minute),
		)

		// First call should hit the store
		_, err := c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)
		assert.Equal(t, int32(1), store.getCount.Load())

		// Second call should use cache
		_, err = c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)
		assert.Equal(t, int32(1), store.getCount.Load())
	})

	t.Run("returns error for invalid reference", func(t *testing.T) {
		t.Parallel()
		c := New()
		_, err := c.AuthHeaders(context.Background(), ":::invalid")
		assert.Error(t, err)
	})
}

func TestInvalidateAuthHeaders(t *testing.T) {
	t.Parallel()

	t.Run("clears cached auth header", func(t *testing.T) {
		t.Parallel()
		store := &countingStore{
			inner: StaticCredentials("registry.example.com", "user", "pass"),
		}
		c := New(
			WithCredentialStore(store),
			WithAuthHeaderCacheTTL(time.Minute),
		)

		// Populate cache
		_, err := c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)
		assert.Equal(t, int32(1), store.getCount.Load())

		// Invalidate
		err = c.InvalidateAuthHeaders("registry.example.com/repo:tag")
		require.NoError(t, err)

		// Next call should hit the store again
		_, err = c.AuthHeaders(context.Background(), "registry.example.com/repo:tag")
		require.NoError(t, err)
		assert.Equal(t, int32(2), store.getCount.Load())
	})

	t.Run("returns error for invalid reference", func(t *testing.T) {
		t.Parallel()
		c := New()
		err := c.InvalidateAuthHeaders(":::invalid")
		assert.Error(t, err)
	})

	t.Run("safe when cache is disabled", func(t *testing.T) {
		t.Parallel()
		c := New(WithAuthHeaderCacheTTL(0))
		err := c.InvalidateAuthHeaders("registry.example.com/repo:tag")
		assert.NoError(t, err)
	})
}

func TestBasicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		username string
		password string
		want     string
	}{
		{
			username: "user",
			password: "pass",
			want:     "Basic dXNlcjpwYXNz",
		},
		{
			username: "admin",
			password: "",
			want:     "Basic YWRtaW46",
		},
		{
			username: "",
			password: "secret",
			want:     "Basic OnNlY3JldA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.username+":"+tt.password, func(t *testing.T) {
			t.Parallel()
			result := basicAuth(tt.username, tt.password)
			assert.Equal(t, tt.want, result)
		})
	}
}

// countingStore wraps a credential store to count Get calls.
type countingStore struct {
	inner interface {
		Get(context.Context, string) (auth.Credential, error)
	}
	getCount atomic.Int32
}

func (s *countingStore) Get(ctx context.Context, serverAddress string) (auth.Credential, error) {
	s.getCount.Add(1)
	return s.inner.Get(ctx, serverAddress)
}

func (s *countingStore) Put(_ context.Context, _ string, _ auth.Credential) error {
	return nil
}

func (s *countingStore) Delete(_ context.Context, _ string) error {
	return nil
}

func TestMapError(t *testing.T) {
	t.Parallel()

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, mapError(nil))
	})

	t.Run("errdef.ErrNotFound maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()
		err := mapError(errdef.ErrNotFound)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("errcode.ErrorResponse 404 maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()
		resp := &errcode.ErrorResponse{
			StatusCode: 404,
		}
		err := mapError(resp)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("errcode.ErrorResponse 401 maps to ErrUnauthorized", func(t *testing.T) {
		t.Parallel()
		resp := &errcode.ErrorResponse{
			StatusCode: 401,
		}
		err := mapError(resp)
		assert.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("errcode.ErrorResponse 403 maps to ErrForbidden", func(t *testing.T) {
		t.Parallel()
		resp := &errcode.ErrorResponse{
			StatusCode: 403,
		}
		err := mapError(resp)
		assert.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("unknown error passes through", func(t *testing.T) {
		t.Parallel()
		originalErr := errors.New("some random error")
		err := mapError(originalErr)
		assert.Equal(t, originalErr, err)
	})

	t.Run("wrapped errdef.ErrNotFound maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("wrapped: %w", errdef.ErrNotFound)
		err := mapError(wrapped)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}
