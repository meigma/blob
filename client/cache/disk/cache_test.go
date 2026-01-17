package disk

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestRefCachePutGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(filepath.Join(dir, "refs"))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	ref := "registry.example.com/repo:v1.0.0"
	digest := "sha256:abc123def456"

	if err := c.PutDigest(ref, digest); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}

	got, ok := c.GetDigest(ref)
	if !ok {
		t.Fatal("GetDigest() ok = false, want true")
	}
	if got != digest {
		t.Fatalf("GetDigest() = %q, want %q", got, digest)
	}

	// Verify sharded path
	sum := sha256.Sum256([]byte(ref))
	hexHash := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, "refs", hexHash[:defaultShardPrefixLen], hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestRefCacheNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir)
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	_, ok := c.GetDigest("nonexistent")
	if ok {
		t.Fatal("GetDigest() ok = true, want false for nonexistent ref")
	}
}

func TestRefCacheShardDisable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	ref := "registry.example.com/repo:v2.0.0"
	digest := "sha256:xyz789"

	if err := c.PutDigest(ref, digest); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}

	sum := sha256.Sum256([]byte(ref))
	hexHash := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestRefCacheAlreadyCached(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir)
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	ref := "registry.example.com/repo:v1.0.0"
	digest := "sha256:abc123"

	if err := c.PutDigest(ref, digest); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}
	if err := c.PutDigest(ref, digest); err != nil { // Should be no-op
		t.Fatalf("PutDigest() error = %v", err)
	}

	got, ok := c.GetDigest(ref)
	if !ok {
		t.Fatal("GetDigest() ok = false, want true")
	}
	if got != digest {
		t.Fatalf("GetDigest() = %q, want %q", got, digest)
	}
}

func TestRefCacheNewEmptyDir(t *testing.T) {
	t.Parallel()

	if _, err := NewRefCache(""); err == nil {
		t.Fatal("NewRefCache() error = nil, want error")
	}
}

func TestRefCacheNegativeShardLen(t *testing.T) {
	t.Parallel()

	if _, err := NewRefCache(t.TempDir(), WithShardPrefixLen(-1)); err == nil {
		t.Fatal("NewRefCache() error = nil, want error for negative shard len")
	}
}

func TestManifestCachePutGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(filepath.Join(dir, "manifests"))
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	digest := "sha256:abc123def456789"
	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    "sha256:config123",
			Size:      1234,
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageLayerGzip,
				Digest:    "sha256:layer1",
				Size:      5678,
			},
		},
	}

	if err := c.PutManifest(digest, manifest); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	got, ok := c.GetManifest(digest)
	if !ok {
		t.Fatal("GetManifest() ok = false, want true")
	}
	if got.MediaType != manifest.MediaType {
		t.Fatalf("manifest.MediaType = %q, want %q", got.MediaType, manifest.MediaType)
	}
	if got.Config.Digest != manifest.Config.Digest {
		t.Fatalf("manifest.Config.Digest = %q, want %q", got.Config.Digest, manifest.Config.Digest)
	}
	if len(got.Layers) != len(manifest.Layers) {
		t.Fatalf("len(manifest.Layers) = %d, want %d", len(got.Layers), len(manifest.Layers))
	}

	// Verify sharded path (digest without sha256: prefix)
	hexHash := "abc123def456789"
	path := filepath.Join(dir, "manifests", hexHash[:defaultShardPrefixLen], hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestManifestCacheNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir)
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	_, ok := c.GetManifest("sha256:deadbeef")
	if ok {
		t.Fatal("GetManifest() ok = true, want false for nonexistent digest")
	}
}

func TestManifestCacheShardDisable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	digest := "sha256:deadbeef"
	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	if err := c.PutManifest(digest, manifest); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	hexHash := "deadbeef"
	path := filepath.Join(dir, hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestManifestCacheAlreadyCached(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir)
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	digest := "sha256:abc123"
	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	if err := c.PutManifest(digest, manifest); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}
	if err := c.PutManifest(digest, manifest); err != nil { // Should be no-op
		t.Fatalf("PutManifest() error = %v", err)
	}

	got, ok := c.GetManifest(digest)
	if !ok {
		t.Fatal("GetManifest() ok = false, want true")
	}
	if got.MediaType != manifest.MediaType {
		t.Fatalf("manifest.MediaType = %q, want %q", got.MediaType, manifest.MediaType)
	}
}

func TestManifestCacheNewEmptyDir(t *testing.T) {
	t.Parallel()

	if _, err := NewManifestCache(""); err == nil {
		t.Fatal("NewManifestCache() error = nil, want error")
	}
}

func TestManifestCacheNegativeShardLen(t *testing.T) {
	t.Parallel()

	if _, err := NewManifestCache(t.TempDir(), WithShardPrefixLen(-1)); err == nil {
		t.Fatal("NewManifestCache() error = nil, want error for negative shard len")
	}
}

func TestManifestCacheCorruptedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	// Write corrupted JSON directly to cache
	hexHash := "deadbeef1234"
	path := filepath.Join(dir, hexHash)
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, ok := c.GetManifest("sha256:" + hexHash)
	if ok {
		t.Fatal("GetManifest() ok = true, want false for corrupted JSON")
	}
}

func TestManifestCacheRejectsInvalidDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	digest := "sha256:../escape"
	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	if err := c.PutManifest(digest, manifest); err == nil {
		t.Fatal("PutManifest() error = nil, want error for invalid digest")
	}

	if _, ok := c.GetManifest(digest); ok {
		t.Fatal("GetManifest() ok = true, want false for invalid digest")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty cache dir, got %d entries", len(entries))
	}
}

func TestWithDirPerm(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subdir := filepath.Join(dir, "refs")

	_, err := NewRefCache(subdir, WithDirPerm(0o755))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	info, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	// Check that directory was created with specified permissions
	// Note: umask may affect actual permissions
	if info.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("directory perm = %o, want at least 0700", info.Mode().Perm())
	}
}
