package disk

import (
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

const (
	defaultShardPrefixLen = 2
	defaultDirPerm        = 0o700
)

// Cache implements cache.Cache using the local filesystem.
// Files are stored in a directory hierarchy with optional sharding by hash prefix.
// The cache is safe for concurrent use.
type Cache struct {
	dir            string       // root directory for cached files
	shardPrefixLen int          // number of hex chars for subdirectory sharding
	dirPerm        os.FileMode  // permissions for created directories
	maxBytes       int64        // maximum cache size (0 = unlimited)
	bytes          atomic.Int64 // current total size of cached files
	pruneMu        sync.Mutex   // serializes prune operations
}

// Option configures a disk cache.
type Option func(*Cache)

// WithShardPrefixLen sets the number of hex characters used for sharding.
// Use 0 to disable sharding. Defaults to 2.
func WithShardPrefixLen(n int) Option {
	return func(c *Cache) {
		c.shardPrefixLen = n
	}
}

// WithDirPerm sets the directory permissions used for cache directories.
func WithDirPerm(mode os.FileMode) Option {
	return func(c *Cache) {
		c.dirPerm = mode
	}
}

// WithMaxBytes sets the maximum cache size in bytes.
// Values < 0 are invalid. Use 0 to disable the limit.
func WithMaxBytes(n int64) Option {
	return func(c *Cache) {
		c.maxBytes = n
	}
}

// New creates a disk-backed cache rooted at dir.
func New(dir string, opts ...Option) (*Cache, error) {
	if dir == "" {
		return nil, errors.New("cache dir is empty")
	}
	c := &Cache{
		dir:            dir,
		shardPrefixLen: defaultShardPrefixLen,
		dirPerm:        defaultDirPerm,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.shardPrefixLen < 0 {
		return nil, errors.New("shard prefix length must be >= 0")
	}
	if c.maxBytes < 0 {
		return nil, errors.New("max bytes must be >= 0")
	}
	if err := os.MkdirAll(dir, c.dirPerm); err != nil {
		return nil, err
	}
	if size, err := dirSize(dir); err == nil {
		c.bytes.Store(size)
	} else {
		return nil, err
	}
	return c, nil
}

// Get returns an fs.File for reading cached content.
// Returns nil, false if the content is not cached.
func (c *Cache) Get(hash []byte) (fs.File, bool) {
	path, err := c.path(hash)
	if err != nil {
		return nil, false
	}
	f, err := os.Open(path) //nolint:gosec // path is derived from hash, not user input
	if err != nil {
		return nil, false
	}
	return f, true
}

// Put stores content by reading from the provided fs.File.
// The cache reads the file to completion; caller still owns/closes the file.
func (c *Cache) Put(hash []byte, f fs.File) error {
	path, err := c.path(hash)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return nil
	}

	dir := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(dir, c.dirPerm); mkdirErr != nil {
		return mkdirErr
	}

	tmp, err := os.CreateTemp(dir, "cache-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	written, err := io.Copy(tmp, f)
	if err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if ok, err := c.ensureCapacity(written); err != nil {
		_ = os.Remove(tmpPath)
		return err
	} else if !ok {
		_ = os.Remove(tmpPath)
		return nil
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = os.Remove(tmpPath)
			return nil
		}
		_ = os.Remove(tmpPath)
		return err
	}
	c.bytes.Add(written)
	return nil
}

// Delete removes cached content for the given hash.
func (c *Cache) Delete(hash []byte) error {
	path, err := c.path(hash)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return statErr
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info != nil {
		c.bytes.Add(-info.Size())
	}
	return nil
}

// MaxBytes returns the configured cache size limit (0 = unlimited).
func (c *Cache) MaxBytes() int64 {
	return c.maxBytes
}

// SizeBytes returns the current cache size in bytes.
func (c *Cache) SizeBytes() int64 {
	return c.bytes.Load()
}

// Prune removes cached entries until the cache is at or below targetBytes.
func (c *Cache) Prune(targetBytes int64) (int64, error) {
	if targetBytes < 0 {
		targetBytes = 0
	}
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()

	freed, remaining, err := pruneDir(c.dir, targetBytes)
	if err != nil {
		return 0, err
	}
	c.bytes.Store(remaining)
	return freed, nil
}

func (c *Cache) path(hash []byte) (string, error) {
	if len(hash) == 0 {
		return "", errors.New("hash is empty")
	}
	hexHash := hex.EncodeToString(hash)
	if c.shardPrefixLen <= 0 {
		return filepath.Join(c.dir, hexHash), nil
	}
	prefixLen := c.shardPrefixLen
	if prefixLen > len(hexHash) {
		prefixLen = len(hexHash)
	}
	return filepath.Join(c.dir, hexHash[:prefixLen], hexHash), nil
}

func (c *Cache) ensureCapacity(need int64) (bool, error) {
	if c.maxBytes <= 0 {
		return true, nil
	}
	if need > c.maxBytes {
		return false, nil
	}
	if c.SizeBytes()+need <= c.maxBytes {
		return true, nil
	}
	if _, err := c.Prune(c.maxBytes - need); err != nil {
		return false, err
	}
	return c.SizeBytes()+need <= c.maxBytes, nil
}
