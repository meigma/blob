package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	t.Parallel()

	// Create test directory with files
	dir := t.TempDir()
	files := map[string]string{
		"a.txt":         "content of a",
		"b.txt":         "content of b",
		"sub/c.txt":     "content of c",
		"sub/deep/d.go": "package main",
	}
	createTestFiles(t, dir, files)

	// Create archive
	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	// Load index and verify (using internal loadIndex for testing)
	idx, err := loadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	assert.Equal(t, 4, idx.len())

	// Verify entries are sorted by path
	paths := make([]string, 0, idx.len())
	for view := range idx.entriesView() {
		paths = append(paths, view.Path())
	}
	assert.Equal(t, []string{"a.txt", "b.txt", "sub/c.txt", "sub/deep/d.go"}, paths)

	// Verify we can look up each file and data is correct
	for path, content := range files {
		path = filepath.ToSlash(path)
		view, ok := idx.lookupView(path)
		require.True(t, ok, "entry %q not found", path)

		// Verify data content
		data := dataBuf.Bytes()[view.DataOffset() : view.DataOffset()+view.DataSize()]
		assert.Equal(t, content, string(data), "content mismatch for %q", path)

		// Verify hash
		expectedHash := sha256.Sum256([]byte(content))
		assert.Equal(t, expectedHash[:], view.HashBytes(), "hash mismatch for %q", path)
	}
}

func TestCreateEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := loadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	assert.Equal(t, 0, idx.len())
	assert.Equal(t, 0, dataBuf.Len())
}

func TestCreateCompression(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Use repetitive content that compresses well
	content := bytes.Repeat([]byte("hello world "), 1000)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), content, 0o644))

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf, CreateWithCompression(CompressionZstd))
	require.NoError(t, err)

	idx, err := loadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	view, ok := idx.lookupView("test.txt")
	require.True(t, ok)

	// Compressed should be smaller than original
	assert.Less(t, view.DataSize(), view.OriginalSize(), "compressed size should be smaller")
	assert.Equal(t, CompressionZstd, view.Compression())

	// Verify we can decompress
	compressed := dataBuf.Bytes()[view.DataOffset() : view.DataOffset()+view.DataSize()]
	dec, err := zstd.NewReader(nil)
	require.NoError(t, err)
	decompressed, err := dec.DecodeAll(compressed, nil)
	require.NoError(t, err)

	assert.Equal(t, content, decompressed)

	// Verify hash is of uncompressed content
	expectedHash := sha256.Sum256(content)
	assert.Equal(t, expectedHash[:], view.HashBytes())
}

func TestCreateMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0o755))

	// Set a specific mod time
	modTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(filePath, modTime, modTime))

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := loadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	view, ok := idx.lookupView("test.txt")
	require.True(t, ok)

	assert.Equal(t, fs.FileMode(0o755), view.Mode())
	assert.True(t, view.ModTime().Equal(modTime), "ModTime mismatch: expected %v, got %v", modTime, view.ModTime())
}

func TestCreateSkipsSymlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("real"), 0o644))
	require.NoError(t, os.Symlink("real.txt", filepath.Join(dir, "link.txt")))

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := loadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	// Only real file should be present
	assert.Equal(t, 1, idx.len())
	_, ok := idx.lookupView("real.txt")
	assert.True(t, ok)
	_, ok = idx.lookupView("link.txt")
	assert.False(t, ok)
}

func TestCreateCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create several files
	for i := range 10 {
		name := filepath.Join(dir, string(rune('a'+i))+".txt")
		require.NoError(t, os.WriteFile(name, []byte("content"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	var indexBuf, dataBuf bytes.Buffer
	err := Create(ctx, dir, &indexBuf, &dataBuf)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestCreatePrefixScans(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"assets/css/main.css":    "css1",
		"assets/css/reset.css":   "css2",
		"assets/images/logo.png": "png1",
		"src/main.go":            "go1",
	}
	createTestFiles(t, dir, files)

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := loadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	// Verify prefix scan works correctly
	cssPaths := make([]string, 0, 2)
	for view := range idx.entriesWithPrefixView("assets/css/") {
		cssPaths = append(cssPaths, view.Path())
	}
	assert.Equal(t, []string{"assets/css/main.css", "assets/css/reset.css"}, cssPaths)
}

// createTestFiles creates files in dir from a map of relative path to content.
func createTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
	}
}
