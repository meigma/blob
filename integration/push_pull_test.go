//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meigma/blob"
	blobcore "github.com/meigma/blob/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Push Operations ---

func TestPush_Basic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "push-basic")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Verify manifest was created by fetching it
	manifest, err := client.Fetch(ctx, ref)
	require.NoError(t, err, "Fetch")
	require.NotNil(t, manifest)
	assert.NotEmpty(t, manifest.Digest(), "manifest digest")
}

func TestPush_WithCompression(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, compressibleArchive)

	ref := testRef(registryAddr, "push-compression")
	err := client.Push(ctx, ref, dir, blob.PushWithCompression(blob.CompressionZstd))
	require.NoError(t, err, "Push with compression")

	// Pull and verify content is decompressed correctly
	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, archive, compressibleArchive)
}

func TestPush_WithTags(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRefWithTag(registryAddr, "push-tags", "v1")
	err := client.Push(ctx, ref, dir,
		blob.PushWithTags("latest", "v1.0.0"),
	)
	require.NoError(t, err, "Push with tags")

	// Verify all tags point to the same manifest
	manifest1, err := client.Fetch(ctx, testRefWithTag(registryAddr, "push-tags", "v1"))
	require.NoError(t, err, "Fetch v1")

	manifest2, err := client.Fetch(ctx, testRefWithTag(registryAddr, "push-tags", "latest"))
	require.NoError(t, err, "Fetch latest")

	manifest3, err := client.Fetch(ctx, testRefWithTag(registryAddr, "push-tags", "v1.0.0"))
	require.NoError(t, err, "Fetch v1.0.0")

	assert.Equal(t, manifest1.Digest(), manifest2.Digest(), "v1 and latest should match")
	assert.Equal(t, manifest1.Digest(), manifest3.Digest(), "v1 and v1.0.0 should match")
}

func TestPush_WithAnnotations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	annotations := map[string]string{
		"org.opencontainers.image.title":   "Test Archive",
		"org.opencontainers.image.version": "1.0.0",
		"custom.annotation":                "custom-value",
	}

	ref := testRef(registryAddr, "push-annotations")
	err := client.Push(ctx, ref, dir, blob.PushWithAnnotations(annotations))
	require.NoError(t, err, "Push with annotations")

	manifest, err := client.Fetch(ctx, ref)
	require.NoError(t, err, "Fetch")

	ann := manifest.Annotations()
	for key, value := range annotations {
		assert.Equal(t, value, ann[key], "annotation %q", key)
	}
}

func TestPush_WithChangeDetection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "push-change-detection")
	err := client.Push(ctx, ref, dir,
		blob.PushWithChangeDetection(blob.ChangeDetectionStrict),
	)
	require.NoError(t, err, "Push with change detection")

	// Pull and verify
	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, archive, smallArchive)
}

func TestPush_WithMaxFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create more files than the limit
	manyFiles := make(map[string][]byte)
	for i := range 10 {
		manyFiles[filepath.Join("dir", strings.Repeat("x", i+1)+".txt")] = []byte("content")
	}

	dir := t.TempDir()
	createTestFiles(t, dir, manyFiles)

	ref := testRef(registryAddr, "push-max-files")
	err := client.Push(ctx, ref, dir, blob.PushWithMaxFiles(5))

	// Should fail because there are more files than the limit
	require.Error(t, err, "Push should fail with too many files")
	assert.ErrorIs(t, err, blob.ErrTooManyFiles)
}

func TestPush_WithSkipCompression(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	files := map[string][]byte{
		"image.jpg": makeRandomContent(1024), // should skip (pre-compressed format)
		"data.txt":  makeCompressibleContent(10 * 1024),
		"tiny.txt":  []byte("small"), // should skip (too small)
	}

	dir := t.TempDir()
	createTestFiles(t, dir, files)

	ref := testRef(registryAddr, "push-skip-compression")
	err := client.Push(ctx, ref, dir,
		blob.PushWithCompression(blob.CompressionZstd),
		blob.PushWithSkipCompression(blob.DefaultSkipCompression(1024)),
	)
	require.NoError(t, err, "Push with skip compression")

	// Pull and verify content
	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, archive, files)
}

// --- Pull Operations ---

func TestPull_Basic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Setup: push first
	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "pull-basic")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Pull and verify
	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, archive, nestedArchive)
	assertArchiveLen(t, archive, len(nestedArchive))
}

func TestPull_LazyLoading(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create large files that would be expensive to download all at once
	largeFiles := map[string][]byte{
		"file1.bin": makeRandomContent(100 * 1024),
		"file2.bin": makeRandomContent(100 * 1024),
		"file3.bin": makeRandomContent(100 * 1024),
	}

	dir := t.TempDir()
	createTestFiles(t, dir, largeFiles)

	ref := testRef(registryAddr, "pull-lazy")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Pull (should be fast as data is loaded lazily)
	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	// Only read one file - others shouldn't be downloaded
	content, err := archive.ReadFile("file1.bin")
	require.NoError(t, err, "ReadFile")
	assert.Equal(t, largeFiles["file1.bin"], content)
}

func TestPull_WithSkipCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)

	cacheDir := t.TempDir()
	client := newTestClient(t, registryAddr, blob.WithCacheDir(cacheDir))

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "pull-skip-cache")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// First pull (populates cache)
	_, err = client.Pull(ctx, ref)
	require.NoError(t, err, "First Pull")

	// Second pull with skip cache (should fetch fresh)
	archive, err := client.Pull(ctx, ref, blob.PullWithSkipCache())
	require.NoError(t, err, "Pull with skip cache")
	assertFilesMatch(t, archive, smallArchive)
}

func TestPull_WithMaxFileSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create archive with a large file
	files := map[string][]byte{
		"large.bin": makeRandomContent(100 * 1024), // 100KB
		"small.txt": []byte("small"),
	}

	dir := t.TempDir()
	createTestFiles(t, dir, files)

	ref := testRef(registryAddr, "pull-max-file-size")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Pull with a small max file size
	archive, err := client.Pull(ctx, ref, blob.PullWithMaxFileSize(1024)) // 1KB limit
	require.NoError(t, err, "Pull")

	// Small file should work
	content, err := archive.ReadFile("small.txt")
	require.NoError(t, err, "ReadFile small.txt")
	assert.Equal(t, []byte("small"), content)

	// Large file should fail
	_, err = archive.ReadFile("large.bin")
	assert.Error(t, err, "ReadFile large.bin should fail")
}

func TestPull_WithDecoderConcurrency(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, compressibleArchive)

	ref := testRef(registryAddr, "pull-decoder-concurrency")
	err := client.Push(ctx, ref, dir, blob.PushWithCompression(blob.CompressionZstd))
	require.NoError(t, err, "Push")

	// Pull with specific decoder concurrency
	archive, err := client.Pull(ctx, ref, blob.PullWithDecoderConcurrency(4))
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, archive, compressibleArchive)
}

// --- Fetch & Tag Operations ---

func TestFetch_ManifestOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "fetch-manifest-only")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Fetch manifest without downloading data
	manifest, err := client.Fetch(ctx, ref)
	require.NoError(t, err, "Fetch")

	assert.NotNil(t, manifest)
	assert.NotEmpty(t, manifest.Digest)
	assert.NotNil(t, manifest.Annotations)
}

func TestFetch_Metadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "fetch-metadata")
	err := client.Push(ctx, ref, dir, blob.PushWithCompression(blob.CompressionZstd))
	require.NoError(t, err, "Push")

	manifest, err := client.Fetch(ctx, ref)
	require.NoError(t, err, "Fetch")

	// Verify metadata fields
	assert.True(t, strings.HasPrefix(manifest.Digest(), "sha256:"), "digest format")
	assert.Greater(t, manifest.IndexDescriptor().Size, int64(0), "index size")
	assert.Greater(t, manifest.DataDescriptor().Size, int64(0), "data size")
}

func TestTag_CreateAlias(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRefWithTag(registryAddr, "tag-alias", "v1")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Get the manifest digest
	manifest, err := client.Fetch(ctx, ref)
	require.NoError(t, err, "Fetch")

	// Create a new tag pointing to the same manifest
	aliasRef := testRefWithTag(registryAddr, "tag-alias", "production")
	err = client.Tag(ctx, aliasRef, manifest.Digest())
	require.NoError(t, err, "Tag")

	// Verify the alias points to the same manifest
	aliasManifest, err := client.Fetch(ctx, aliasRef)
	require.NoError(t, err, "Fetch alias")
	assert.Equal(t, manifest.Digest(), aliasManifest.Digest())
}

// --- Round-Trip Tests (Table-Driven) ---

func TestPushPull_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string][]byte
		compression blob.Compression
	}{
		{
			name:        "small_flat",
			files:       smallArchive,
			compression: blob.CompressionNone,
		},
		{
			name:        "nested_dirs",
			files:       nestedArchive,
			compression: blob.CompressionNone,
		},
		{
			name:        "compressed",
			files:       compressibleArchive,
			compression: blob.CompressionZstd,
		},
		{
			name: "binary_content",
			files: map[string][]byte{
				"random.bin": makeRandomContent(4096),
			},
			compression: blob.CompressionNone,
		},
		{
			name: "deep_nesting",
			files: map[string][]byte{
				"a/b/c/d/e/f/g/h/i/j/file.txt": []byte("deeply nested"),
			},
			compression: blob.CompressionNone,
		},
		{
			name:        "many_files",
			files:       makeManySmallFiles(50),
			compression: blob.CompressionZstd,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			registryAddr := getRegistry(t)
			client := newTestClient(t, registryAddr)

			dir := t.TempDir()
			createTestFiles(t, dir, tc.files)

			ref := testRef(registryAddr, "roundtrip-"+tc.name)

			var pushOpts []blob.PushOption
			if tc.compression != blob.CompressionNone {
				pushOpts = append(pushOpts, blob.PushWithCompression(tc.compression))
			}

			err := client.Push(ctx, ref, dir, pushOpts...)
			require.NoError(t, err, "Push")

			archive, err := client.Pull(ctx, ref)
			require.NoError(t, err, "Pull")

			assertFilesMatch(t, archive, tc.files)
			assertArchiveLen(t, archive, len(tc.files))
		})
	}
}

// --- Caching Tests ---

func TestClient_WithCacheDir(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)

	cacheDir := t.TempDir()
	client := newTestClient(t, registryAddr, blob.WithCacheDir(cacheDir))

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "cache-dir")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, archive, nestedArchive)

	// Verify cache directories were created
	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err, "ReadDir cache")
	assert.NotEmpty(t, entries, "cache directory should have subdirectories")
}

func TestClient_CacheHitPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)

	cacheDir := t.TempDir()
	client := newTestClient(t, registryAddr, blob.WithCacheDir(cacheDir))

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "cache-hit")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// First pull (cache miss)
	archive1, err := client.Pull(ctx, ref)
	require.NoError(t, err, "First Pull")
	content1, err := archive1.ReadFile("hello.txt")
	require.NoError(t, err, "ReadFile on first pull")

	// Second pull (cache hit)
	archive2, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Second Pull")
	content2, err := archive2.ReadFile("hello.txt")
	require.NoError(t, err, "ReadFile on second pull")

	assert.Equal(t, content1, content2, "content should match")
}

// --- PushArchive Tests ---

func TestPushArchive_PreCreated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	// Create archive in memory first
	var indexBuf, dataBuf bytes.Buffer
	err := blobcore.Create(ctx, dir, &indexBuf, &dataBuf)
	require.NoError(t, err, "Create archive")

	archive, err := blobcore.New(indexBuf.Bytes(), &memSource{data: dataBuf.Bytes()})
	require.NoError(t, err, "New blob")

	// Push pre-created archive
	ref := testRef(registryAddr, "push-archive")
	err = client.PushArchive(ctx, ref, archive)
	require.NoError(t, err, "PushArchive")

	// Pull and verify
	pulled, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")
	assertFilesMatch(t, pulled, smallArchive)
}

// --- Helper Functions ---

// makeManySmallFiles creates a map of n small files.
func makeManySmallFiles(n int) map[string][]byte {
	files := make(map[string][]byte, n)
	for i := range n {
		dir := filepath.Join("dir"+string(rune('a'+i%26)), "subdir")
		name := filepath.Join(dir, fmt.Sprintf("file%d.txt", i))
		files[name] = []byte(fmt.Sprintf("content %d", i))
	}
	return files
}

// memSource implements blobcore.ByteSource for in-memory data.
type memSource struct {
	data []byte
}

// ReadAt reads len(p) bytes into p starting at offset off in the underlying data.
func (m *memSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}
	return n, nil
}

// Size returns the total size of the in-memory data.
func (m *memSource) Size() int64 {
	return int64(len(m.data))
}

// SourceID returns a fixed identifier for the memory source.
func (m *memSource) SourceID() string {
	return "memory"
}
