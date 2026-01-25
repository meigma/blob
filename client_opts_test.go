package blob

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_DefaultRefCacheTTL(t *testing.T) {
	t.Parallel()

	client, err := NewClient()
	require.NoError(t, err)

	assert.Equal(t, DefaultRefCacheTTL, client.refCacheTTL)
}

func TestWithRefCacheTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ttl     time.Duration
		wantTTL time.Duration
		wantErr string
	}{
		{
			name:    "positive TTL",
			ttl:     10 * time.Minute,
			wantTTL: 10 * time.Minute,
		},
		{
			name:    "zero TTL disables expiration",
			ttl:     0,
			wantTTL: 0,
		},
		{
			name:    "negative TTL rejected",
			ttl:     -5 * time.Minute,
			wantErr: "ref cache TTL must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(WithRefCacheTTL(tt.ttl))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTTL, client.refCacheTTL)
		})
	}
}

func TestWithCacheDir_AppliesRefCacheTTL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Set custom TTL before WithCacheDir
	customTTL := 15 * time.Minute
	client, err := NewClient(
		WithRefCacheTTL(customTTL),
		WithCacheDir(dir),
	)
	require.NoError(t, err)
	require.NotNil(t, client.refCache)

	// Verify the cache was created (we can't directly inspect the TTL,
	// but we can verify no error occurred during creation)
	assert.Equal(t, customTTL, client.refCacheTTL)
}

func TestWithCacheDir_UsesDefaultTTL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Don't set WithRefCacheTTL - should use default
	client, err := NewClient(WithCacheDir(dir))
	require.NoError(t, err)
	require.NotNil(t, client.refCache)

	// Verify the default TTL is set on the client
	assert.Equal(t, DefaultRefCacheTTL, client.refCacheTTL)
}

func TestWithRefCacheDir_AppliesRefCacheTTL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Set custom TTL before WithRefCacheDir
	customTTL := 20 * time.Minute
	client, err := NewClient(
		WithRefCacheTTL(customTTL),
		WithRefCacheDir(filepath.Join(dir, "refs")),
	)
	require.NoError(t, err)
	require.NotNil(t, client.refCache)

	assert.Equal(t, customTTL, client.refCacheTTL)
}

func TestWithRefCacheTTL_OrderMatters(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Setting TTL after cache creation still stores the value on client,
	// but the cache was already created with the default TTL
	client, err := NewClient(
		WithCacheDir(dir),
		WithRefCacheTTL(30*time.Minute), // This comes too late for the cache
	)
	require.NoError(t, err)
	require.NotNil(t, client.refCache)

	// The client's TTL field is updated, but the cache was already created
	// with DefaultRefCacheTTL. This test documents the order-dependent behavior.
	assert.Equal(t, 30*time.Minute, client.refCacheTTL)
}

func TestWithCacheDir_CreatesBlockCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	client, err := NewClient(WithCacheDir(dir))
	require.NoError(t, err)
	assert.NotNil(t, client.blockCache, "block cache should be created by WithCacheDir")
}

func TestWithBlockCacheDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	client, err := NewClient(WithBlockCacheDir(filepath.Join(dir, "blocks")))
	require.NoError(t, err)
	assert.NotNil(t, client.blockCache)
}
