package oras

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthHeaderCache(t *testing.T) {
	t.Parallel()

	t.Run("positive TTL creates cache", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCache(time.Minute)
		require.NotNil(t, cache)
		assert.Equal(t, time.Minute, cache.ttl)
		assert.Equal(t, defaultAuthHeaderCacheMaxSize, cache.maxSize)
		assert.NotNil(t, cache.entries)
		assert.NotNil(t, cache.order)
	})

	t.Run("custom max size", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCacheWithSize(time.Minute, 50)
		require.NotNil(t, cache)
		assert.Equal(t, 50, cache.maxSize)
	})

	t.Run("non-positive max size uses default", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCacheWithSize(time.Minute, 0)
		require.NotNil(t, cache)
		assert.Equal(t, defaultAuthHeaderCacheMaxSize, cache.maxSize)
	})

	t.Run("zero TTL returns nil", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCache(0)
		assert.Nil(t, cache)
	})

	t.Run("negative TTL returns nil", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCache(-time.Second)
		assert.Nil(t, cache)
	})
}

func TestAuthHeaderCache_GetSet(t *testing.T) {
	// No t.Parallel() - subtests share cache
	cache := newAuthHeaderCache(time.Minute)
	require.NotNil(t, cache)

	t.Run("get returns false for missing key", func(t *testing.T) {
		value, ok := cache.get("unknown-host")
		assert.False(t, ok)
		assert.Empty(t, value)
	})

	t.Run("set and get returns value", func(t *testing.T) {
		cache.set("example.com", "Bearer token123")

		value, ok := cache.get("example.com")
		assert.True(t, ok)
		assert.Equal(t, "Bearer token123", value)
	})

	t.Run("set overwrites existing value", func(t *testing.T) {
		cache.set("overwrite.com", "Bearer old")
		cache.set("overwrite.com", "Bearer new")

		value, ok := cache.get("overwrite.com")
		assert.True(t, ok)
		assert.Equal(t, "Bearer new", value)
	})

	t.Run("different hosts have different values", func(t *testing.T) {
		cache.set("host1.com", "Bearer token1")
		cache.set("host2.com", "Bearer token2")

		value1, ok1 := cache.get("host1.com")
		assert.True(t, ok1)
		assert.Equal(t, "Bearer token1", value1)

		value2, ok2 := cache.get("host2.com")
		assert.True(t, ok2)
		assert.Equal(t, "Bearer token2", value2)
	})

	t.Run("empty value can be cached", func(t *testing.T) {
		cache.set("anon.example.com", "")

		value, ok := cache.get("anon.example.com")
		assert.True(t, ok)
		assert.Empty(t, value)
	})
}

func TestAuthHeaderCache_Expiration(t *testing.T) {
	// No t.Parallel() - time-sensitive tests
	t.Run("entry expires after TTL", func(t *testing.T) {
		// Use a very short TTL
		cache := newAuthHeaderCache(10 * time.Millisecond)
		require.NotNil(t, cache)

		cache.set("example.com", "Bearer token")

		// Immediately available
		value, ok := cache.get("example.com")
		assert.True(t, ok)
		assert.Equal(t, "Bearer token", value)

		// Wait for expiration
		time.Sleep(20 * time.Millisecond)

		// Should be expired
		value, ok = cache.get("example.com")
		assert.False(t, ok)
		assert.Empty(t, value)
	})

	t.Run("expired entry is removed from map", func(t *testing.T) {
		cache := newAuthHeaderCache(10 * time.Millisecond)
		require.NotNil(t, cache)

		cache.set("example.com", "Bearer token")

		// Wait for expiration
		time.Sleep(20 * time.Millisecond)

		// Get triggers removal
		_, _ = cache.get("example.com")

		cache.mu.Lock()
		_, exists := cache.entries["example.com"]
		cache.mu.Unlock()

		assert.False(t, exists)
	})
}

func TestAuthHeaderCache_Invalidate(t *testing.T) {
	t.Parallel()

	t.Run("invalidate removes entry", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCache(time.Minute)
		require.NotNil(t, cache)

		cache.set("example.com", "Bearer token")
		cache.invalidate("example.com")

		value, ok := cache.get("example.com")
		assert.False(t, ok)
		assert.Empty(t, value)
	})

	t.Run("invalidate non-existent key is safe", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCache(time.Minute)
		require.NotNil(t, cache)

		// Should not panic
		cache.invalidate("non-existent")
	})

	t.Run("invalidate only affects specified host", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCache(time.Minute)
		require.NotNil(t, cache)

		cache.set("host1.com", "Bearer token1")
		cache.set("host2.com", "Bearer token2")

		cache.invalidate("host1.com")

		_, ok1 := cache.get("host1.com")
		assert.False(t, ok1)

		value2, ok2 := cache.get("host2.com")
		assert.True(t, ok2)
		assert.Equal(t, "Bearer token2", value2)
	})
}

func TestAuthHeaderCache_LRUEviction(t *testing.T) {
	t.Parallel()

	t.Run("evicts oldest when at capacity", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCacheWithSize(time.Minute, 3)
		require.NotNil(t, cache)

		cache.set("host1.com", "token1")
		cache.set("host2.com", "token2")
		cache.set("host3.com", "token3")

		// Cache is full, adding another should evict host1 (oldest)
		cache.set("host4.com", "token4")

		_, ok1 := cache.get("host1.com")
		assert.False(t, ok1, "host1 should have been evicted")

		// Others should still be present
		_, ok2 := cache.get("host2.com")
		assert.True(t, ok2)
		_, ok3 := cache.get("host3.com")
		assert.True(t, ok3)
		_, ok4 := cache.get("host4.com")
		assert.True(t, ok4)
	})

	t.Run("get promotes entry to front", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCacheWithSize(time.Minute, 3)
		require.NotNil(t, cache)

		cache.set("host1.com", "token1")
		cache.set("host2.com", "token2")
		cache.set("host3.com", "token3")

		// Access host1, making it most recently used
		_, _ = cache.get("host1.com")

		// Adding new entry should evict host2 (now oldest)
		cache.set("host4.com", "token4")

		_, ok1 := cache.get("host1.com")
		assert.True(t, ok1, "host1 should still be present after access")

		_, ok2 := cache.get("host2.com")
		assert.False(t, ok2, "host2 should have been evicted")
	})

	t.Run("update promotes entry to front", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCacheWithSize(time.Minute, 3)
		require.NotNil(t, cache)

		cache.set("host1.com", "token1")
		cache.set("host2.com", "token2")
		cache.set("host3.com", "token3")

		// Update host1, making it most recently used
		cache.set("host1.com", "token1-updated")

		// Adding new entry should evict host2 (now oldest)
		cache.set("host4.com", "token4")

		value1, ok1 := cache.get("host1.com")
		assert.True(t, ok1, "host1 should still be present after update")
		assert.Equal(t, "token1-updated", value1)

		_, ok2 := cache.get("host2.com")
		assert.False(t, ok2, "host2 should have been evicted")
	})

	t.Run("size stays bounded", func(t *testing.T) {
		t.Parallel()
		cache := newAuthHeaderCacheWithSize(time.Minute, 5)
		require.NotNil(t, cache)

		// Add more entries than max size
		for i := range 10 {
			cache.set(fmt.Sprintf("host%d.com", i), fmt.Sprintf("token%d", i))
		}

		cache.mu.Lock()
		size := len(cache.entries)
		listLen := cache.order.Len()
		cache.mu.Unlock()

		assert.Equal(t, 5, size, "map should have exactly maxSize entries")
		assert.Equal(t, 5, listLen, "list should have exactly maxSize entries")
	})
}
