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
	"sync"
	"sync/atomic"

	digest "github.com/opencontainers/go-digest"
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
	maxBytes       int64
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

// WithMaxBytes sets the maximum cache size in bytes.
// Values < 0 are invalid. Use 0 to disable the limit.
func WithMaxBytes(n int64) Option {
	return func(c *config) {
		c.maxBytes = n
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
	maxBytes       int64
	bytes          atomic.Int64
	pruneMu        sync.Mutex
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
	if cfg.maxBytes < 0 {
		return nil, errors.New("max bytes must be >= 0")
	}
	if err := os.MkdirAll(dir, cfg.dirPerm); err != nil {
		return nil, err
	}
	c := &RefCache{
		dir:            dir,
		shardPrefixLen: cfg.shardPrefixLen,
		dirPerm:        cfg.dirPerm,
		maxBytes:       cfg.maxBytes,
	}
	if size, err := dirSize(dir); err == nil {
		c.bytes.Store(size)
	} else {
		return nil, err
	}
	return c, nil
}

// GetDigest returns the digest for a reference if cached.
//
// The cached digest is validated to match the expected algorithm:hex format.
// Invalid entries are automatically deleted to prevent cache poisoning.
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

	// Validate digest format: must be "algorithm:hex"
	digest = string(data)
	if !isValidDigestFormat(digest) {
		_ = c.deleteByPath(root, path)
		return "", false
	}
	return digest, true
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

	written := int64(len(digest))
	if ok, err := c.ensureCapacity(written); err != nil {
		return err
	} else if !ok {
		return nil // Cache full, skip silently
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

	c.bytes.Add(written)
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

// Delete removes a cached reference.
func (c *RefCache) Delete(ref string) error {
	path := c.path(ref)
	fullPath := filepath.Join(c.dir, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(fullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	c.bytes.Add(-info.Size())
	return nil
}

// MaxBytes returns the configured cache size limit (0 = unlimited).
func (c *RefCache) MaxBytes() int64 {
	return c.maxBytes
}

// SizeBytes returns the current cache size in bytes.
func (c *RefCache) SizeBytes() int64 {
	return c.bytes.Load()
}

// Prune removes cached entries until the cache is at or below targetBytes.
func (c *RefCache) Prune(targetBytes int64) (int64, error) {
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

func (c *RefCache) ensureCapacity(need int64) (bool, error) {
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

func (c *RefCache) deleteByPath(root *os.Root, path string) error {
	info, err := root.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := root.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	c.bytes.Add(-info.Size())
	return nil
}

// isValidDigestFormat checks if a string matches the "algorithm:hex" format.
func isValidDigestFormat(s string) bool {
	idx := strings.IndexByte(s, ':')
	if idx < 1 || idx == len(s)-1 {
		return false
	}
	algo := s[:idx]
	hex := s[idx+1:]
	// Algorithm must match [A-Za-z0-9_+.-]+
	for i := 0; i < len(algo); i++ {
		ch := algo[i]
		if !((ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '+' || ch == '-' || ch == '.' || ch == '_') {
			return false
		}
	}
	// Hex part must be valid hex characters
	for i := 0; i < len(hex); i++ {
		ch := hex[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return len(hex) > 0
}

// ManifestCache stores digest->manifest mappings on disk.
//
// Digests are used directly as filenames (with the algorithm prefix stripped),
// and manifests are stored as raw JSON bytes. Cached manifests are validated
// against their digest on read to prevent cache poisoning.
type ManifestCache struct {
	dir            string
	shardPrefixLen int
	dirPerm        os.FileMode
	maxBytes       int64
	bytes          atomic.Int64
	pruneMu        sync.Mutex
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
	if cfg.maxBytes < 0 {
		return nil, errors.New("max bytes must be >= 0")
	}
	if err := os.MkdirAll(dir, cfg.dirPerm); err != nil {
		return nil, err
	}
	c := &ManifestCache{
		dir:            dir,
		shardPrefixLen: cfg.shardPrefixLen,
		dirPerm:        cfg.dirPerm,
		maxBytes:       cfg.maxBytes,
	}
	if size, err := dirSize(dir); err == nil {
		c.bytes.Store(size)
	} else {
		return nil, err
	}
	return c, nil
}

// GetManifest returns the cached manifest for a digest.
//
// Corrupted cache entries (digest mismatch or invalid JSON) are automatically deleted.
func (c *ManifestCache) GetManifest(dgst string) (manifest *ocispec.Manifest, ok bool) {
	path, err := c.path(dgst)
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

	match, err := digestMatches(dgst, data)
	if err != nil || !match {
		_ = c.deleteByPath(root, path)
		return nil, false
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		_ = c.deleteByPath(root, path)
		return nil, false
	}
	return &m, true
}

// PutManifest caches raw manifest bytes by digest.
func (c *ManifestCache) PutManifest(dgst string, raw []byte) error {
	path, err := c.path(dgst)
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

	match, err := digestMatches(dgst, raw)
	if err != nil {
		return err
	}
	if !match {
		return fmt.Errorf("manifest digest mismatch for %q", dgst)
	}

	written := int64(len(raw))
	if ok, err := c.ensureCapacity(written); err != nil {
		return err
	} else if !ok {
		return nil // Cache full, skip silently
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := root.MkdirAll(dir, c.dirPerm); err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
	}

	tmp, tmpPath, err := createTemp(root, dir, "manifest-*")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}

	if _, err := tmp.Write(raw); err != nil {
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

	c.bytes.Add(written)
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

// Delete removes a cached manifest.
func (c *ManifestCache) Delete(digest string) error {
	path, err := c.path(digest)
	if err != nil {
		return err
	}
	fullPath := filepath.Join(c.dir, path)
	info, statErr := os.Stat(fullPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return statErr
	}
	if err := os.Remove(fullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	c.bytes.Add(-info.Size())
	return nil
}

// MaxBytes returns the configured cache size limit (0 = unlimited).
func (c *ManifestCache) MaxBytes() int64 {
	return c.maxBytes
}

// SizeBytes returns the current cache size in bytes.
func (c *ManifestCache) SizeBytes() int64 {
	return c.bytes.Load()
}

// Prune removes cached entries until the cache is at or below targetBytes.
func (c *ManifestCache) Prune(targetBytes int64) (int64, error) {
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

func (c *ManifestCache) ensureCapacity(need int64) (bool, error) {
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

func (c *ManifestCache) deleteByPath(root *os.Root, path string) error {
	info, err := root.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := root.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	c.bytes.Add(-info.Size())
	return nil
}

// IndexCache stores digest->index blob mappings on disk.
//
// Digests are used directly as filenames (with the algorithm prefix stripped),
// and index blobs are stored as raw bytes. Cached entries are validated
// against their digest on read to prevent cache poisoning.
type IndexCache struct {
	dir            string
	shardPrefixLen int
	dirPerm        os.FileMode
	maxBytes       int64
	bytes          atomic.Int64
	pruneMu        sync.Mutex
}

// NewIndexCache creates a disk-backed index cache rooted at dir.
func NewIndexCache(dir string, opts ...Option) (*IndexCache, error) {
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
	if cfg.maxBytes < 0 {
		return nil, errors.New("max bytes must be >= 0")
	}
	if err := os.MkdirAll(dir, cfg.dirPerm); err != nil {
		return nil, err
	}
	c := &IndexCache{
		dir:            dir,
		shardPrefixLen: cfg.shardPrefixLen,
		dirPerm:        cfg.dirPerm,
		maxBytes:       cfg.maxBytes,
	}
	if size, err := dirSize(dir); err == nil {
		c.bytes.Store(size)
	} else {
		return nil, err
	}
	return c, nil
}

// GetIndex returns the cached index bytes for a digest.
//
// Corrupted cache entries (digest mismatch) are automatically deleted.
func (c *IndexCache) GetIndex(dgst string) (index []byte, ok bool) {
	path, err := c.path(dgst)
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

	match, err := digestMatches(dgst, data)
	if err != nil || !match {
		_ = c.deleteByPath(root, path)
		return nil, false
	}

	return data, true
}

// PutIndex caches raw index bytes by digest.
func (c *IndexCache) PutIndex(dgst string, raw []byte) error {
	path, err := c.path(dgst)
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

	match, err := digestMatches(dgst, raw)
	if err != nil {
		return err
	}
	if !match {
		return fmt.Errorf("index digest mismatch for %q", dgst)
	}

	written := int64(len(raw))
	if ok, err := c.ensureCapacity(written); err != nil {
		return err
	} else if !ok {
		return nil // Cache full, skip silently
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := root.MkdirAll(dir, c.dirPerm); err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
	}

	tmp, tmpPath, err := createTemp(root, dir, "index-*")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}

	if _, err := tmp.Write(raw); err != nil {
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

	c.bytes.Add(written)
	return nil
}

func (c *IndexCache) path(digest string) (string, error) {
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

// Delete removes a cached index blob.
func (c *IndexCache) Delete(digest string) error {
	path, err := c.path(digest)
	if err != nil {
		return err
	}
	fullPath := filepath.Join(c.dir, path)
	info, statErr := os.Stat(fullPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return statErr
	}
	if err := os.Remove(fullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	c.bytes.Add(-info.Size())
	return nil
}

// MaxBytes returns the configured cache size limit (0 = unlimited).
func (c *IndexCache) MaxBytes() int64 {
	return c.maxBytes
}

// SizeBytes returns the current cache size in bytes.
func (c *IndexCache) SizeBytes() int64 {
	return c.bytes.Load()
}

// Prune removes cached entries until the cache is at or below targetBytes.
func (c *IndexCache) Prune(targetBytes int64) (int64, error) {
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

func (c *IndexCache) ensureCapacity(need int64) (bool, error) {
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

func (c *IndexCache) deleteByPath(root *os.Root, path string) error {
	info, err := root.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := root.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	c.bytes.Add(-info.Size())
	return nil
}

func digestMatches(dgst string, data []byte) (bool, error) {
	parsed, err := digest.Parse(dgst)
	if err != nil {
		return false, fmt.Errorf("parse digest %q: %w", dgst, err)
	}
	if err := parsed.Validate(); err != nil {
		return false, fmt.Errorf("validate digest %q: %w", dgst, err)
	}
	algo := parsed.Algorithm()
	if !algo.Available() {
		return false, fmt.Errorf("digest algorithm %q unavailable", algo)
	}
	computed := algo.FromBytes(data)
	return computed == parsed, nil
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
