//go:build integration

package integration

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Error Scenarios ---

func TestError_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Try to pull non-existent archive
	ref := testRef(registryAddr, "nonexistent-archive-12345")
	_, err := client.Pull(ctx, ref)

	require.Error(t, err, "Pull should fail")
	assert.ErrorIs(t, err, blob.ErrNotFound)
}

func TestError_InvalidReference(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Try to use an invalid reference
	invalidRefs := []string{
		"not-a-valid-ref",
		"://missing-scheme",
		"",
	}

	for _, ref := range invalidRefs {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			_, err := client.Fetch(ctx, ref)
			assert.Error(t, err, "Fetch should fail with invalid ref")
		})
	}
}

func TestError_TooManyFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create many files
	manyFiles := make(map[string][]byte)
	for i := range 100 {
		manyFiles[filepath.Join("dir", string(rune('a'+i%26)), string(rune('0'+i%10))+".txt")] = []byte("x")
	}

	dir := t.TempDir()
	createTestFiles(t, dir, manyFiles)

	ref := testRef(registryAddr, "error-too-many-files")
	err := client.Push(ctx, ref, dir, blob.PushWithMaxFiles(10))

	require.Error(t, err, "Push should fail")
	assert.ErrorIs(t, err, blob.ErrTooManyFiles)
}

func TestError_ReadFile_NotExist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "error-readfile-notexist")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	// Try to read non-existent file
	_, err = archive.ReadFile("nonexistent.txt")
	require.Error(t, err)

	var pathErr *fs.PathError
	if assert.ErrorAs(t, err, &pathErr) {
		assert.ErrorIs(t, pathErr.Err, fs.ErrNotExist)
	}
}

func TestError_ReadDir_NotExist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "error-readdir-notexist")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	// Try to read non-existent directory
	_, err = archive.ReadDir("nonexistent-dir")
	require.Error(t, err)

	var pathErr *fs.PathError
	if assert.ErrorAs(t, err, &pathErr) {
		assert.ErrorIs(t, pathErr.Err, fs.ErrNotExist)
	}
}

func TestError_Open_InvalidPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "error-open-invalid")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	// Try to open with path traversal
	invalidPaths := []string{
		"../escape",
		"./hello/../../../etc/passwd",
		"/absolute/path",
	}

	for _, path := range invalidPaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			_, err := archive.Open(path)
			require.Error(t, err, "Open should fail for %q", path)
		})
	}
}

func TestError_IndexTooLarge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create many files to have a larger index
	manyFiles := make(map[string][]byte)
	for i := range 50 {
		path := filepath.Join("dir", string(rune('a'+i%26)), string(rune('0'+i%10))+".txt")
		manyFiles[path] = []byte("content")
	}

	dir := t.TempDir()
	createTestFiles(t, dir, manyFiles)

	ref := testRef(registryAddr, "error-index-too-large")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	// Try to pull with very small max index size
	_, err = client.Pull(ctx, ref, blob.PullWithMaxIndexSize(100))
	assert.Error(t, err, "Pull should fail with max index size exceeded")
}

func TestError_Symlink(t *testing.T) {
	t.Parallel()

	// Skip on Windows where symlinks require special permissions
	if os.Getenv("SKIP_SYMLINK_TESTS") == "1" {
		t.Skip("SKIP_SYMLINK_TESTS is set")
	}

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, map[string][]byte{
		"target.txt": []byte("target content"),
	})

	// Create a symlink
	linkPath := filepath.Join(dir, "link.txt")
	targetPath := filepath.Join(dir, "target.txt")
	err := os.Symlink(targetPath, linkPath)
	if err != nil {
		t.Skip("Failed to create symlink:", err)
	}

	ref := testRef(registryAddr, "error-symlink")
	err = client.Push(ctx, ref, dir)

	// Symlinks are silently skipped during archive creation
	require.NoError(t, err, "Push should succeed (symlinks are skipped)")

	// Pull and verify symlink was not included
	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	// Only target.txt should be in the archive, not the symlink
	assert.Equal(t, 1, archive.Len(), "archive should only have target file")
	_, err = archive.ReadFile("link.txt")
	assert.Error(t, err, "symlink should not be in archive")
}

func TestError_Fetch_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	ref := testRef(registryAddr, "error-fetch-notfound-67890")
	_, err := client.Fetch(ctx, ref)

	require.Error(t, err, "Fetch should fail")
	assert.ErrorIs(t, err, blob.ErrNotFound)
}

func TestError_Tag_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Try to tag a non-existent digest
	ref := testRef(registryAddr, "error-tag-notfound")
	err := client.Tag(ctx, ref, "sha256:0000000000000000000000000000000000000000000000000000000000000000")

	require.Error(t, err, "Tag should fail")
}

func TestError_CopyTo_InvalidPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "error-copyto-invalid")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()

	// CopyTo with invalid paths should silently skip them
	_, err = archive.CopyTo(destDir, "../escape", "hello.txt")
	require.NoError(t, err, "CopyTo should not error on invalid paths")

	// But valid file should still be copied
	_, err = os.Stat(filepath.Join(destDir, "hello.txt"))
	require.NoError(t, err, "hello.txt should be copied")
}
