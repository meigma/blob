package oras

import (
	"container/list"
	"sync"
	"time"
)

const (
	defaultAuthHeaderCacheTTL     = time.Minute
	defaultAuthHeaderCacheMaxSize = 100
)

// authHeaderCache is an LRU cache for auth headers with TTL expiration.
// It provides thread-safe access to cached authorization headers, evicting
// entries based on both time-to-live and least-recently-used policies.
type authHeaderCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	entries map[string]*list.Element
	order   *list.List // front = most recently used
}

// cachedAuthHeader represents a single cached authorization header entry.
type cachedAuthHeader struct {
	host    string
	value   string
	expires time.Time
}

// newAuthHeaderCache creates a new auth header cache with the specified TTL
// and the default maximum size. Returns nil if ttl is zero or negative.
func newAuthHeaderCache(ttl time.Duration) *authHeaderCache {
	return newAuthHeaderCacheWithSize(ttl, defaultAuthHeaderCacheMaxSize)
}

// newAuthHeaderCacheWithSize creates a new auth header cache with the specified
// TTL and maximum size. Returns nil if ttl is zero or negative. If maxSize is
// zero or negative, the default maximum size is used.
func newAuthHeaderCacheWithSize(ttl time.Duration, maxSize int) *authHeaderCache {
	if ttl <= 0 {
		return nil
	}
	if maxSize <= 0 {
		maxSize = defaultAuthHeaderCacheMaxSize
	}
	return &authHeaderCache{
		ttl:     ttl,
		maxSize: maxSize,
		entries: make(map[string]*list.Element),
		order:   list.New(),
	}
}

// get retrieves the cached authorization header for the given host.
// Returns the header value and true if found and not expired, or an empty
// string and false otherwise. Accessing an entry promotes it to the front
// of the LRU list.
func (c *authHeaderCache) get(host string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[host]
	if !ok {
		return "", false
	}

	entry := elem.Value.(*cachedAuthHeader) //nolint:errcheck // type is guaranteed by set
	if time.Now().After(entry.expires) {
		c.removeLocked(elem, host)
		return "", false
	}

	// Move to front (most recently used)
	c.order.MoveToFront(elem)
	return entry.value, true
}

// set stores an authorization header for the given host. If an entry already
// exists, it is updated and promoted to the front. If the cache is at capacity,
// the least recently used entry is evicted before adding the new one.
func (c *authHeaderCache) set(host, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry
	if elem, ok := c.entries[host]; ok {
		entry := elem.Value.(*cachedAuthHeader) //nolint:errcheck // type is guaranteed
		entry.value = value
		entry.expires = time.Now().Add(c.ttl)
		c.order.MoveToFront(elem)
		return
	}

	// Evict oldest if at capacity
	for c.order.Len() >= c.maxSize {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		oldEntry := oldest.Value.(*cachedAuthHeader) //nolint:errcheck // type is guaranteed
		c.removeLocked(oldest, oldEntry.host)
	}

	// Add new entry
	entry := &cachedAuthHeader{
		host:    host,
		value:   value,
		expires: time.Now().Add(c.ttl),
	}
	elem := c.order.PushFront(entry)
	c.entries[host] = elem
}

// invalidate removes the cached authorization header for the given host.
// It is safe to call this method even if the host is not in the cache.
func (c *authHeaderCache) invalidate(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.entries[host]; ok {
		c.removeLocked(elem, host)
	}
}

// removeLocked removes an element from both the list and map.
// Caller must hold c.mu.
func (c *authHeaderCache) removeLocked(elem *list.Element, host string) {
	c.order.Remove(elem)
	delete(c.entries, host)
}
