// Package disk provides a disk-backed cache implementation.
package disk

import (
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	defaultShardPrefixLen = 2
	defaultDirPerm        = 0o700
)

// Cache implements cache.Cache using the local filesystem.
type Cache struct {
	dir            string
	shardPrefixLen int
	dirPerm        os.FileMode
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
	if err := os.MkdirAll(dir, c.dirPerm); err != nil {
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

	if _, err := io.Copy(tmp, f); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = os.Remove(tmpPath)
			return nil
		}
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// Delete removes cached content for the given hash.
func (c *Cache) Delete(hash []byte) error {
	path, err := c.path(hash)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
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
