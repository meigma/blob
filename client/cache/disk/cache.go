// Package disk provides disk-backed implementations of client cache interfaces.
package disk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	defaultShardPrefixLen = 2
	defaultDirPerm        = 0o700
)

// config holds shared configuration for disk caches.
type config struct {
	shardPrefixLen int
	dirPerm        os.FileMode
}

// Option configures a disk cache.
type Option func(*config)

// WithShardPrefixLen sets the number of hex characters used for sharding.
// Use 0 to disable sharding. Defaults to 2.
func WithShardPrefixLen(n int) Option {
	return func(c *config) {
		c.shardPrefixLen = n
	}
}

// WithDirPerm sets the directory permissions used for cache directories.
func WithDirPerm(mode os.FileMode) Option {
	return func(c *config) {
		c.dirPerm = mode
	}
}

func defaultConfig() config {
	return config{
		shardPrefixLen: defaultShardPrefixLen,
		dirPerm:        defaultDirPerm,
	}
}

// RefCache stores ref->digest mappings on disk.
//
// References are hashed with SHA256 to create safe filenames, since refs
// contain special characters like ':', '/', and '@'.
type RefCache struct {
	dir            string
	shardPrefixLen int
	dirPerm        os.FileMode
}

// NewRefCache creates a disk-backed ref cache rooted at dir.
func NewRefCache(dir string, opts ...Option) (*RefCache, error) {
	if dir == "" {
		return nil, errors.New("cache dir is empty")
	}
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.shardPrefixLen < 0 {
		return nil, errors.New("shard prefix length must be >= 0")
	}
	if err := os.MkdirAll(dir, cfg.dirPerm); err != nil {
		return nil, err
	}
	return &RefCache{
		dir:            dir,
		shardPrefixLen: cfg.shardPrefixLen,
		dirPerm:        cfg.dirPerm,
	}, nil
}

// GetDigest returns the digest for a reference if cached.
func (c *RefCache) GetDigest(ref string) (digest string, ok bool) {
	path := c.path(ref)
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return "", false
	}
	defer root.Close()

	data, err := root.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// PutDigest caches a reference to digest mapping.
func (c *RefCache) PutDigest(ref, digest string) error {
	path := c.path(ref)
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return fmt.Errorf("open cache root: %w", err)
	}
	defer root.Close()

	if _, err := root.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat cache entry: %w", err)
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := root.MkdirAll(dir, c.dirPerm); err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
	}

	tmp, tmpPath, err := createTemp(root, dir, "ref-*")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}

	if _, err := tmp.WriteString(digest); err != nil {
		_ = tmp.Close()
		_ = root.Remove(tmpPath)
		return fmt.Errorf("write cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = root.Remove(tmpPath)
		return fmt.Errorf("close cache file: %w", err)
	}

	if err := root.Rename(tmpPath, path); err != nil {
		if _, statErr := root.Stat(path); statErr == nil {
			_ = root.Remove(tmpPath)
			return nil
		}
		_ = root.Remove(tmpPath)
		return fmt.Errorf("rename cache file: %w", err)
	}

	return nil
}

func (c *RefCache) path(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	hexHash := hex.EncodeToString(sum[:])
	if c.shardPrefixLen <= 0 {
		return hexHash
	}
	prefixLen := min(c.shardPrefixLen, len(hexHash))
	return filepath.Join(hexHash[:prefixLen], hexHash)
}

// ManifestCache stores digest->manifest mappings on disk.
//
// Digests are used directly as filenames (with the algorithm prefix stripped),
// and manifests are stored as JSON.
type ManifestCache struct {
	dir            string
	shardPrefixLen int
	dirPerm        os.FileMode
}

// NewManifestCache creates a disk-backed manifest cache rooted at dir.
func NewManifestCache(dir string, opts ...Option) (*ManifestCache, error) {
	if dir == "" {
		return nil, errors.New("cache dir is empty")
	}
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.shardPrefixLen < 0 {
		return nil, errors.New("shard prefix length must be >= 0")
	}
	if err := os.MkdirAll(dir, cfg.dirPerm); err != nil {
		return nil, err
	}
	return &ManifestCache{
		dir:            dir,
		shardPrefixLen: cfg.shardPrefixLen,
		dirPerm:        cfg.dirPerm,
	}, nil
}

// GetManifest returns the cached manifest for a digest.
func (c *ManifestCache) GetManifest(digest string) (manifest *ocispec.Manifest, ok bool) {
	path, err := c.path(digest)
	if err != nil {
		return nil, false
	}

	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return nil, false
	}
	defer root.Close()

	data, err := root.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return &m, true
}

// PutManifest caches a manifest by its digest.
func (c *ManifestCache) PutManifest(digest string, manifest *ocispec.Manifest) error {
	path, err := c.path(digest)
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return fmt.Errorf("open cache root: %w", err)
	}
	defer root.Close()

	if _, err := root.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat cache entry: %w", err)
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := root.MkdirAll(dir, c.dirPerm); err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	tmp, tmpPath, err := createTemp(root, dir, "manifest-*")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = root.Remove(tmpPath)
		return fmt.Errorf("write cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = root.Remove(tmpPath)
		return fmt.Errorf("close cache file: %w", err)
	}

	if err := root.Rename(tmpPath, path); err != nil {
		if _, statErr := root.Stat(path); statErr == nil {
			_ = root.Remove(tmpPath)
			return nil
		}
		_ = root.Remove(tmpPath)
		return fmt.Errorf("rename cache file: %w", err)
	}

	return nil
}

func (c *ManifestCache) path(digest string) (string, error) {
	// Strip algorithm prefix (e.g., "sha256:") to get hex hash
	hexHash, err := sanitizeHexDigest(digest)
	if err != nil {
		return "", err
	}
	if c.shardPrefixLen <= 0 {
		return hexHash, nil
	}
	prefixLen := min(c.shardPrefixLen, len(hexHash))
	return filepath.Join(hexHash[:prefixLen], hexHash), nil
}

func sanitizeHexDigest(digest string) (string, error) {
	hexHash := digest
	if idx := strings.IndexByte(digest, ':'); idx >= 0 {
		hexHash = digest[idx+1:]
	}
	if hexHash == "" {
		return "", fmt.Errorf("invalid digest %q", digest)
	}
	for i := 0; i < len(hexHash); i++ {
		ch := hexHash[i]
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return "", fmt.Errorf("invalid digest %q", digest)
		}
	}
	return hexHash, nil
}

func createTemp(root *os.Root, dir, pattern string) (*os.File, string, error) {
	if pattern == "" {
		pattern = "tmp"
	}
	if !strings.Contains(pattern, "*") {
		pattern += "*"
	}
	if dir == "" {
		dir = "."
	}

	for tries := 0; tries < 10000; tries++ {
		var randBytes [8]byte
		if _, err := rand.Read(randBytes[:]); err != nil {
			return nil, "", err
		}
		name := strings.Replace(pattern, "*", hex.EncodeToString(randBytes[:]), 1)
		path := filepath.Join(dir, name)
		f, err := root.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return f, path, nil
	}

	return nil, "", errors.New("failed to create temp file")
}
