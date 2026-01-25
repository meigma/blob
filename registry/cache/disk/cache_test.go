package disk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
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
	dgst := "sha256:abc123def456"

	if err := c.PutDigest(ref, dgst); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}

	got, ok := c.GetDigest(ref)
	if !ok {
		t.Fatal("GetDigest() ok = false, want true")
	}
	if got != dgst {
		t.Fatalf("GetDigest() = %q, want %q", got, dgst)
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
	dgst := "sha256:xyz789"

	if err := c.PutDigest(ref, dgst); err != nil {
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
	dgst := "sha256:abc123"

	if err := c.PutDigest(ref, dgst); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}
	if err := c.PutDigest(ref, dgst); err != nil { // Should be no-op
		t.Fatalf("PutDigest() error = %v", err)
	}

	got, ok := c.GetDigest(ref)
	if !ok {
		t.Fatal("GetDigest() ok = false, want true")
	}
	if got != dgst {
		t.Fatalf("GetDigest() = %q, want %q", got, dgst)
	}
}

func TestRefCacheTTLExpiresEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ttl := 2 * time.Second
	c, err := NewRefCache(dir, WithRefCacheTTL(ttl))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	ref := "registry.example.com/repo:v1.0.1"
	dgst := "sha256:deadbeef"

	if err := c.PutDigest(ref, dgst); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}

	path := filepath.Join(c.dir, c.path(ref))
	expired := time.Now().Add(-2 * ttl)
	if err := os.Chtimes(path, expired, expired); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	if _, ok := c.GetDigest(ref); ok {
		t.Fatal("GetDigest() ok = true, want false for expired entry")
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected cache file to be deleted, got err=%v", err)
	}
}

func TestRefCacheTTLWithinWindow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ttl := time.Minute
	c, err := NewRefCache(dir, WithRefCacheTTL(ttl))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	ref := "registry.example.com/repo:v1.0.2"
	dgst := "sha256:cafebabe"

	if err := c.PutDigest(ref, dgst); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}

	path := filepath.Join(c.dir, c.path(ref))
	recent := time.Now().Add(-ttl / 2)
	if err := os.Chtimes(path, recent, recent); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	got, ok := c.GetDigest(ref)
	if !ok {
		t.Fatal("GetDigest() ok = false, want true for unexpired entry")
	}
	if got != dgst {
		t.Fatalf("GetDigest() = %q, want %q", got, dgst)
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

func TestRefCacheNegativeTTL(t *testing.T) {
	t.Parallel()

	if _, err := NewRefCache(t.TempDir(), WithRefCacheTTL(-time.Second)); err == nil {
		t.Fatal("NewRefCache() error = nil, want error for negative ttl")
	}
}

func TestManifestCachePutGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(filepath.Join(dir, "manifests"))
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

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

	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	dgst := digest.FromBytes(raw)

	if err := c.PutManifest(dgst.String(), raw); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	got, gotRaw, ok := c.GetManifest(dgst.String())
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
	if !bytes.Equal(gotRaw, raw) {
		t.Fatalf("GetManifest() raw mismatch")
	}

	// Verify sharded path (digest without sha256: prefix)
	hexHash := dgst.Encoded()
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

	_, _, ok := c.GetManifest("sha256:deadbeef")
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

	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	dgst := digest.FromBytes(raw)

	if err := c.PutManifest(dgst.String(), raw); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	hexHash := dgst.Encoded()
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

	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	dgst := digest.FromBytes(raw)

	if err := c.PutManifest(dgst.String(), raw); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}
	if err := c.PutManifest(dgst.String(), raw); err != nil { // Should be no-op
		t.Fatalf("PutManifest() error = %v", err)
	}

	got, _, ok := c.GetManifest(dgst.String())
	if !ok {
		t.Fatal("GetManifest() ok = false, want true")
	}
	if got.MediaType != manifest.MediaType {
		t.Fatalf("manifest.MediaType = %q, want %q", got.MediaType, manifest.MediaType)
	}
}

func TestIndexCachePutGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(filepath.Join(dir, "indexes"))
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	indexData := []byte("index data")
	dgst := digest.FromBytes(indexData)

	if err := c.PutIndex(dgst.String(), indexData); err != nil {
		t.Fatalf("PutIndex() error = %v", err)
	}

	got, ok := c.GetIndex(dgst.String())
	if !ok {
		t.Fatal("GetIndex() ok = false, want true")
	}
	if !bytes.Equal(got, indexData) {
		t.Fatalf("GetIndex() = %q, want %q", got, indexData)
	}

	hexHash := dgst.Encoded()
	path := filepath.Join(dir, "indexes", hexHash[:defaultShardPrefixLen], hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestIndexCacheNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir)
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	_, ok := c.GetIndex("sha256:deadbeef")
	if ok {
		t.Fatal("GetIndex() ok = true, want false for nonexistent digest")
	}
}

func TestIndexCacheShardDisable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	indexData := []byte("index data")
	dgst := digest.FromBytes(indexData)

	if err := c.PutIndex(dgst.String(), indexData); err != nil {
		t.Fatalf("PutIndex() error = %v", err)
	}

	hexHash := dgst.Encoded()
	path := filepath.Join(dir, hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestIndexCacheAlreadyCached(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir)
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	indexData := []byte("index data")
	dgst := digest.FromBytes(indexData)

	if err := c.PutIndex(dgst.String(), indexData); err != nil {
		t.Fatalf("PutIndex() error = %v", err)
	}
	if err := c.PutIndex(dgst.String(), indexData); err != nil { // Should be no-op
		t.Fatalf("PutIndex() error = %v", err)
	}

	got, ok := c.GetIndex(dgst.String())
	if !ok {
		t.Fatal("GetIndex() ok = false, want true")
	}
	if !bytes.Equal(got, indexData) {
		t.Fatalf("GetIndex() = %q, want %q", got, indexData)
	}
}

func TestIndexCacheNewEmptyDir(t *testing.T) {
	t.Parallel()

	if _, err := NewIndexCache(""); err == nil {
		t.Fatal("NewIndexCache() error = nil, want error")
	}
}

func TestIndexCacheNegativeShardLen(t *testing.T) {
	t.Parallel()

	if _, err := NewIndexCache(t.TempDir(), WithShardPrefixLen(-1)); err == nil {
		t.Fatal("NewIndexCache() error = nil, want error for negative shard len")
	}
}

func TestIndexCacheRejectsInvalidDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	invalidDigest := "sha256:../escape"
	indexData := []byte("index data")

	if putErr := c.PutIndex(invalidDigest, indexData); putErr == nil {
		t.Fatal("PutIndex() error = nil, want error for invalid digest")
	}

	if _, ok := c.GetIndex(invalidDigest); ok {
		t.Fatal("GetIndex() ok = true, want false for invalid digest")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty cache dir, got %d entries", len(entries))
	}
}

func TestIndexCacheCorruptedDeleted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	clean := []byte("index data")
	dgst := digest.FromBytes(clean)
	path := filepath.Join(dir, dgst.Encoded())
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, ok := c.GetIndex(dgst.String())
	if ok {
		t.Fatal("GetIndex() ok = true, want false for corrupted data")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected corrupted cache file to be deleted")
	}
}

func TestIndexCacheSizeTracking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir)
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	if c.SizeBytes() != 0 {
		t.Fatalf("SizeBytes() = %d, want 0 for empty cache", c.SizeBytes())
	}

	indexData := []byte("index data")
	dgst := digest.FromBytes(indexData)

	if err := c.PutIndex(dgst.String(), indexData); err != nil {
		t.Fatalf("PutIndex() error = %v", err)
	}

	expectedSize := int64(len(indexData))
	if c.SizeBytes() != expectedSize {
		t.Fatalf("SizeBytes() = %d, want %d", c.SizeBytes(), expectedSize)
	}

	if err := c.Delete(dgst.String()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if c.SizeBytes() != 0 {
		t.Fatalf("SizeBytes() = %d, want 0 after delete", c.SizeBytes())
	}
}

func TestIndexCachePrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir)
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	for i := range 3 {
		indexData := []byte{byte('a' + i), byte('b' + i), byte('c' + i)}
		dgst := digest.FromBytes(indexData)
		if putErr := c.PutIndex(dgst.String(), indexData); putErr != nil {
			t.Fatalf("PutIndex() error = %v", putErr)
		}
	}

	sizeBefore := c.SizeBytes()
	if sizeBefore == 0 {
		t.Fatal("SizeBytes() = 0, expected > 0")
	}

	freed, err := c.Prune(sizeBefore / 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if freed == 0 {
		t.Fatal("Prune() freed = 0, expected > 0")
	}
	if c.SizeBytes() >= sizeBefore {
		t.Fatalf("SizeBytes() = %d, want < %d after prune", c.SizeBytes(), sizeBefore)
	}
}

func TestIndexCacheMaxBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewIndexCache(dir, WithMaxBytes(100))
	if err != nil {
		t.Fatalf("NewIndexCache() error = %v", err)
	}

	if c.MaxBytes() != 100 {
		t.Fatalf("MaxBytes() = %d, want 100", c.MaxBytes())
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
	content := []byte("not valid json")
	dgst := digest.FromBytes(content)
	path := filepath.Join(dir, dgst.Encoded())
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, _, ok := c.GetManifest(dgst.String())
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

	invalidDigest := "sha256:../escape"
	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	raw, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		t.Fatalf("Marshal() error = %v", marshalErr)
	}

	if putErr := c.PutManifest(invalidDigest, raw); putErr == nil {
		t.Fatal("PutManifest() error = nil, want error for invalid digest")
	}

	if _, _, ok := c.GetManifest(invalidDigest); ok {
		t.Fatal("GetManifest() ok = true, want false for invalid digest")
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
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

func TestRefCacheInvalidDigestFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	ref := "registry.example.com/repo:v1.0.0"

	// Write invalid digest directly to cache (missing colon)
	sum := sha256.Sum256([]byte(ref))
	hexHash := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, hexHash)
	if err := os.WriteFile(path, []byte("invaliddigest"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// GetDigest should return false and delete the invalid file
	_, ok := c.GetDigest(ref)
	if ok {
		t.Fatal("GetDigest() ok = true, want false for invalid digest format")
	}

	// Verify file was deleted
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected invalid cache file to be deleted")
	}
}

func TestManifestCacheCorruptedDeleted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	// Write corrupted JSON directly to cache
	content := []byte("not valid json")
	dgst := digest.FromBytes(content)
	path := filepath.Join(dir, dgst.Encoded())
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// GetManifest should return false
	_, _, ok := c.GetManifest(dgst.String())
	if ok {
		t.Fatal("GetManifest() ok = true, want false for corrupted JSON")
	}

	// Verify file was deleted
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected corrupted cache file to be deleted")
	}
}

func TestRefCacheSizeTracking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir)
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	if c.SizeBytes() != 0 {
		t.Fatalf("SizeBytes() = %d, want 0 for empty cache", c.SizeBytes())
	}

	ref := "registry.example.com/repo:v1.0.0"
	dgst := "sha256:abc123def456"

	if err := c.PutDigest(ref, dgst); err != nil {
		t.Fatalf("PutDigest() error = %v", err)
	}

	expectedSize := int64(len(dgst))
	if c.SizeBytes() != expectedSize {
		t.Fatalf("SizeBytes() = %d, want %d", c.SizeBytes(), expectedSize)
	}

	// Delete and verify size decreases
	if err := c.Delete(ref); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if c.SizeBytes() != 0 {
		t.Fatalf("SizeBytes() = %d, want 0 after delete", c.SizeBytes())
	}
}

func TestManifestCacheSizeTracking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir)
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	if c.SizeBytes() != 0 {
		t.Fatalf("SizeBytes() = %d, want 0 for empty cache", c.SizeBytes())
	}

	manifest := &ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
	}

	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	dgst := digest.FromBytes(raw)
	if err := c.PutManifest(dgst.String(), raw); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	if c.SizeBytes() == 0 {
		t.Fatal("SizeBytes() = 0, want > 0 after put")
	}

	// Delete and verify size decreases
	if err := c.Delete(dgst.String()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if c.SizeBytes() != 0 {
		t.Fatalf("SizeBytes() = %d, want 0 after delete", c.SizeBytes())
	}
}

func TestRefCachePrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir)
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	// Add multiple entries
	refs := []string{
		"registry.example.com/repo:v1",
		"registry.example.com/repo:v2",
		"registry.example.com/repo:v3",
	}
	for _, ref := range refs {
		if putErr := c.PutDigest(ref, "sha256:abc123"); putErr != nil {
			t.Fatalf("PutDigest() error = %v", putErr)
		}
	}

	sizeBefore := c.SizeBytes()
	if sizeBefore == 0 {
		t.Fatal("SizeBytes() = 0, expected > 0")
	}

	// Prune to half the size
	freed, err := c.Prune(sizeBefore / 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if freed == 0 {
		t.Fatal("Prune() freed = 0, expected > 0")
	}

	if c.SizeBytes() >= sizeBefore {
		t.Fatalf("SizeBytes() = %d, want < %d after prune", c.SizeBytes(), sizeBefore)
	}
}

func TestRefCacheMaxBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewRefCache(dir, WithMaxBytes(100))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	if c.MaxBytes() != 100 {
		t.Fatalf("MaxBytes() = %d, want 100", c.MaxBytes())
	}
}

func TestRefCacheAutoprune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Small cache limit to trigger auto-prune
	c, err := NewRefCache(dir, WithMaxBytes(50))
	if err != nil {
		t.Fatalf("NewRefCache() error = %v", err)
	}

	// Add entries that will exceed the limit
	dgst := "sha256:abc123def456789" // 21 bytes
	for i := range 5 {
		ref := "registry.example.com/repo:v" + string(rune('0'+i))
		if err := c.PutDigest(ref, dgst); err != nil {
			t.Fatalf("PutDigest() error = %v", err)
		}
	}

	// Size should stay at or below limit
	if c.SizeBytes() > c.MaxBytes() {
		t.Fatalf("SizeBytes() = %d > MaxBytes() = %d, expected autoprune", c.SizeBytes(), c.MaxBytes())
	}
}

func TestManifestCachePrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := NewManifestCache(dir)
	if err != nil {
		t.Fatalf("NewManifestCache() error = %v", err)
	}

	// Add multiple entries
	for i := range 3 {
		manifest := &ocispec.Manifest{
			MediaType: ocispec.MediaTypeImageManifest,
			Annotations: map[string]string{
				"cache": string(rune('a' + i)),
			},
		}
		raw, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			t.Fatalf("Marshal() error = %v", marshalErr)
		}
		dgst := digest.FromBytes(raw)
		if putErr := c.PutManifest(dgst.String(), raw); putErr != nil {
			t.Fatalf("PutManifest() error = %v", putErr)
		}
	}

	sizeBefore := c.SizeBytes()
	if sizeBefore == 0 {
		t.Fatal("SizeBytes() = 0, expected > 0")
	}

	// Prune to half the size
	freed, err := c.Prune(sizeBefore / 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if freed == 0 {
		t.Fatal("Prune() freed = 0, expected > 0")
	}

	if c.SizeBytes() >= sizeBefore {
		t.Fatalf("SizeBytes() = %d, want < %d after prune", c.SizeBytes(), sizeBefore)
	}
}

func TestRefCacheNegativeMaxBytes(t *testing.T) {
	t.Parallel()

	if _, err := NewRefCache(t.TempDir(), WithMaxBytes(-1)); err == nil {
		t.Fatal("NewRefCache() error = nil, want error for negative max bytes")
	}
}

func TestManifestCacheNegativeMaxBytes(t *testing.T) {
	t.Parallel()

	if _, err := NewManifestCache(t.TempDir(), WithMaxBytes(-1)); err == nil {
		t.Fatal("NewManifestCache() error = nil, want error for negative max bytes")
	}
}

func TestIndexCacheNegativeMaxBytes(t *testing.T) {
	t.Parallel()

	if _, err := NewIndexCache(t.TempDir(), WithMaxBytes(-1)); err == nil {
		t.Fatal("NewIndexCache() error = nil, want error for negative max bytes")
	}
}
