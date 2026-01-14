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

func TestWriterCreate(t *testing.T) {
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
	w := NewWriter(WriteOptions{Compression: CompressionNone})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	// Load index and verify
	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	assert.Equal(t, 4, idx.Len())

	// Verify entries are sorted by path
	paths := make([]string, 0, idx.Len())
	for entry := range idx.Entries() {
		paths = append(paths, entry.Path)
	}
	assert.Equal(t, []string{"a.txt", "b.txt", "sub/c.txt", "sub/deep/d.go"}, paths)

	// Verify we can look up each file and data is correct
	for path, content := range files {
		path = filepath.ToSlash(path)
		entry, ok := idx.Lookup(path)
		require.True(t, ok, "entry %q not found", path)

		// Verify data content
		data := dataBuf.Bytes()[entry.DataOffset : entry.DataOffset+entry.DataSize]
		assert.Equal(t, content, string(data), "content mismatch for %q", path)

		// Verify hash
		expectedHash := sha256.Sum256([]byte(content))
		assert.Equal(t, expectedHash[:], entry.Hash, "hash mismatch for %q", path)
	}
}

func TestWriterCreateEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var indexBuf, dataBuf bytes.Buffer
	w := NewWriter(WriteOptions{})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	assert.Equal(t, 0, idx.Len())
	assert.Equal(t, 0, dataBuf.Len())
}

func TestWriterCreateCompression(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Use repetitive content that compresses well
	content := bytes.Repeat([]byte("hello world "), 1000)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), content, 0o644))

	var indexBuf, dataBuf bytes.Buffer
	w := NewWriter(WriteOptions{Compression: CompressionZstd})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	entry, ok := idx.Lookup("test.txt")
	require.True(t, ok)

	// Compressed should be smaller than original
	assert.Less(t, entry.DataSize, entry.OriginalSize, "compressed size should be smaller")
	assert.Equal(t, CompressionZstd, entry.Compression)

	// Verify we can decompress
	compressed := dataBuf.Bytes()[entry.DataOffset : entry.DataOffset+entry.DataSize]
	dec, err := zstd.NewReader(nil)
	require.NoError(t, err)
	decompressed, err := dec.DecodeAll(compressed, nil)
	require.NoError(t, err)

	assert.Equal(t, content, decompressed)

	// Verify hash is of uncompressed content
	expectedHash := sha256.Sum256(content)
	assert.Equal(t, expectedHash[:], entry.Hash)
}

func TestWriterCreateMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0o755))

	// Set a specific mod time
	modTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(filePath, modTime, modTime))

	var indexBuf, dataBuf bytes.Buffer
	w := NewWriter(WriteOptions{})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	entry, ok := idx.Lookup("test.txt")
	require.True(t, ok)

	assert.Equal(t, fs.FileMode(0o755), entry.Mode)
	assert.True(t, entry.ModTime.Equal(modTime), "ModTime mismatch: expected %v, got %v", modTime, entry.ModTime)
}

func TestWriterCreateSkipsSymlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("real"), 0o644))
	require.NoError(t, os.Symlink("real.txt", filepath.Join(dir, "link.txt")))

	var indexBuf, dataBuf bytes.Buffer
	w := NewWriter(WriteOptions{})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	// Only real file should be present
	assert.Equal(t, 1, idx.Len())
	_, ok := idx.Lookup("real.txt")
	assert.True(t, ok)
	_, ok = idx.Lookup("link.txt")
	assert.False(t, ok)
}

func TestWriterCreateCancellation(t *testing.T) {
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
	w := NewWriter(WriteOptions{})
	err := w.Create(ctx, dir, &indexBuf, &dataBuf)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestWriterCreatePrefixScans(t *testing.T) {
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
	w := NewWriter(WriteOptions{})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	// Verify prefix scan works correctly
	cssPaths := make([]string, 0, 2)
	for entry := range idx.EntriesWithPrefix("assets/css/") {
		cssPaths = append(cssPaths, entry.Path)
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
