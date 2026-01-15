// Package disk provides a disk-backed cache implementation.
package disk

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/meigma/blob/cache"
)

const (
	defaultShardPrefixLen = 2
	defaultDirPerm        = 0o700
)

// Cache implements cache.Cache and cache.StreamingCache using the local filesystem.
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

// Get retrieves content by its SHA256 hash.
func (c *Cache) Get(hash []byte) ([]byte, bool) {
	path, err := c.path(hash)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from hash, not user input
	if err != nil {
		return nil, false
	}
	return data, true
}

// Put stores content indexed by its SHA256 hash.
func (c *Cache) Put(hash, content []byte) error {
	path, err := c.path(hash)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, c.dirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "cache-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
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

// Writer opens a streaming cache writer for the given hash.
func (c *Cache) Writer(hash []byte) (cache.Writer, error) {
	path, err := c.path(hash)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		return &noopWriter{}, nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, c.dirPerm); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, "cache-*")
	if err != nil {
		return nil, err
	}
	return &diskWriter{
		file:      tmp,
		tmpPath:   tmp.Name(),
		finalPath: path,
	}, nil
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

type diskWriter struct {
	file      *os.File
	tmpPath   string
	finalPath string
}

func (w *diskWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

func (w *diskWriter) Commit() error {
	if err := w.file.Close(); err != nil {
		_ = os.Remove(w.tmpPath)
		return err
	}
	if err := os.Rename(w.tmpPath, w.finalPath); err != nil {
		if _, statErr := os.Stat(w.finalPath); statErr == nil {
			_ = os.Remove(w.tmpPath)
			return nil
		}
		_ = os.Remove(w.tmpPath)
		return err
	}
	return nil
}

func (w *diskWriter) Discard() error {
	if w.file != nil {
		_ = w.file.Close()
	}
	return os.Remove(w.tmpPath)
}

type noopWriter struct{}

func (w *noopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *noopWriter) Commit() error               { return nil }
func (w *noopWriter) Discard() error              { return nil }
