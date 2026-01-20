package gittuf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	gittuflib "github.com/gittuf/gittuf/experimental/gittuf"
)

const (
	defaultCacheDir   = ".cache/blob/gittuf"
	defaultTTL        = time.Hour
	defaultMaxEntries = 10
	metadataFileName  = ".metadata"
	repoSubdirName    = "repo"
)

// RepositoryCache manages cached gittuf repository clones.
// It provides TTL-based expiration and LRU eviction when the
// maximum number of cached repositories is exceeded.
type RepositoryCache struct {
	baseDir    string
	ttl        time.Duration
	maxEntries int
	mu         sync.Mutex
}

// cacheMetadata stores metadata about a cached repository.
type cacheMetadata struct {
	LastUsed time.Time `json:"last_used"`
	RepoURL  string    `json:"repo_url"`
}

// CacheOption configures a RepositoryCache.
type CacheOption func(*RepositoryCache)

// WithCacheBaseDir sets the cache base directory.
func WithCacheBaseDir(dir string) CacheOption {
	return func(c *RepositoryCache) {
		c.baseDir = dir
	}
}

// WithCacheTTLOption sets the cache TTL.
func WithCacheTTLOption(ttl time.Duration) CacheOption {
	return func(c *RepositoryCache) {
		c.ttl = ttl
	}
}

// WithCacheMaxEntries sets the maximum number of cached repositories.
func WithCacheMaxEntries(maxEntries int) CacheOption {
	return func(c *RepositoryCache) {
		c.maxEntries = maxEntries
	}
}

// DefaultCache returns a cache using default settings.
// The cache directory is ~/.cache/blob/gittuf with a 1-hour TTL.
func DefaultCache() *RepositoryCache {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return &RepositoryCache{
		baseDir:    filepath.Join(home, defaultCacheDir),
		ttl:        defaultTTL,
		maxEntries: defaultMaxEntries,
	}
}

// NewRepositoryCache creates a cache with custom settings.
func NewRepositoryCache(opts ...CacheOption) *RepositoryCache {
	c := DefaultCache()
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns a gittuf repository for the given URL.
// If a valid cached clone exists, it is returned.
// Otherwise, a fresh clone is performed.
func (c *RepositoryCache) Get(ctx context.Context, repoURL string) (*gittuflib.Repository, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cacheKey := c.hashURL(repoURL)
	entryDir := filepath.Join(c.baseDir, cacheKey)
	repoPath := filepath.Join(entryDir, repoSubdirName)
	metaPath := filepath.Join(entryDir, metadataFileName)

	// Check if cached clone exists and is valid
	if c.isValidCache(metaPath) {
		repo, err := gittuflib.LoadRepository(repoPath)
		if err == nil {
			c.updateMetadata(metaPath, repoURL)
			return repo, nil
		}
		// Cache corrupted, remove and re-clone
		os.RemoveAll(entryDir)
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		return nil, err
	}

	// Clone with TOFU (nil root keys)
	// The bare=true flag creates a bare clone without a working tree,
	// which is sufficient for verification and uses less disk space.
	repo, err := gittuflib.Clone(ctx, repoURL, repoPath, "", nil, true)
	if err != nil {
		os.RemoveAll(entryDir)
		return nil, err
	}

	c.updateMetadata(metaPath, repoURL)
	c.evictOldEntries()

	return repo, nil
}

// Invalidate removes a cached repository.
func (c *RepositoryCache) Invalidate(repoURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cacheKey := c.hashURL(repoURL)
	entryDir := filepath.Join(c.baseDir, cacheKey)
	return os.RemoveAll(entryDir)
}

// Clear removes all cached repositories.
func (c *RepositoryCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return os.RemoveAll(c.baseDir)
}

// hashURL creates a cache key from a repository URL.
func (c *RepositoryCache) hashURL(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])[:16]
}

// isValidCache checks if the cached repository is still valid.
func (c *RepositoryCache) isValidCache(metaPath string) bool {
	data, err := os.ReadFile(metaPath) //nolint:gosec // path is computed internally
	if err != nil {
		return false
	}

	var meta cacheMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}

	return time.Since(meta.LastUsed) < c.ttl
}

// updateMetadata updates the cache metadata file.
// Metadata write failures are non-fatal; they only affect cache expiration.
func (c *RepositoryCache) updateMetadata(metaPath, repoURL string) {
	meta := cacheMetadata{
		LastUsed: time.Now(),
		RepoURL:  repoURL,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	_ = os.WriteFile(metaPath, data, 0o600) //nolint:errcheck // non-fatal
}

// evictOldEntries removes the oldest entries when maxEntries is exceeded.
func (c *RepositoryCache) evictOldEntries() {
	entries, err := os.ReadDir(c.baseDir)
	if err != nil || len(entries) <= c.maxEntries {
		return
	}

	type entryInfo struct {
		path     string
		lastUsed time.Time
	}

	infos := make([]entryInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(c.baseDir, entry.Name(), metadataFileName)
		data, err := os.ReadFile(metaPath) //nolint:gosec // path is computed internally
		if err != nil {
			continue
		}
		var meta cacheMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		infos = append(infos, entryInfo{
			path:     filepath.Join(c.baseDir, entry.Name()),
			lastUsed: meta.LastUsed,
		})
	}

	// Sort by last used time (oldest first)
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].lastUsed.Before(infos[j].lastUsed)
	})

	// Remove oldest entries until we're under the limit
	for len(infos) > c.maxEntries {
		os.RemoveAll(infos[0].path)
		infos = infos[1:]
	}
}
