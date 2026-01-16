package blob

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileSource(t *testing.T) {
	t.Parallel()

	content := []byte("test file content")
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	source, err := newFileSource(f)
	require.NoError(t, err)

	assert.Equal(t, int64(len(content)), source.Size())

	buf := make([]byte, 4)
	n, err := source.ReadAt(buf, 5)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []byte("file"), buf)
}

func TestOpenFile(t *testing.T) {
	t.Parallel()

	// Create test archive
	files := map[string][]byte{
		"a.txt": []byte("content a"),
		"b.txt": []byte("content b"),
	}
	srcDir := t.TempDir()
	destDir := t.TempDir()

	createTestFilesBytes(t, srcDir, files)

	// Create archive files using Create
	var indexBuf, dataBuf bytes.Buffer
	require.NoError(t, Create(context.Background(), srcDir, &indexBuf, &dataBuf))

	indexPath := filepath.Join(destDir, "index.blob")
	dataPath := filepath.Join(destDir, "data.blob")
	require.NoError(t, os.WriteFile(indexPath, indexBuf.Bytes(), 0o644))
	require.NoError(t, os.WriteFile(dataPath, dataBuf.Bytes(), 0o644))

	// Test OpenFile
	bf, err := OpenFile(indexPath, dataPath)
	require.NoError(t, err)
	defer bf.Close()

	content, err := bf.ReadFile("a.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("content a"), content)

	content, err = bf.ReadFile("b.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("content b"), content)
}

func TestOpenFileNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := OpenFile(filepath.Join(dir, "missing.idx"), filepath.Join(dir, "missing.dat"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read index file")
}

func TestBlobStream(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": []byte("content a"),
		"b.txt": []byte("content b"),
	}
	b := createTestArchive(t, files, CompressionNone)

	// Stream the data
	reader := b.Stream()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	// Data should contain both file contents (sorted by path)
	assert.Contains(t, string(data), "content a")
	assert.Contains(t, string(data), "content b")
}

func TestBlobSave(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("hello world"),
	}
	b := createTestArchive(t, files, CompressionNone)

	destDir := t.TempDir()
	indexPath := filepath.Join(destDir, "saved.idx")
	dataPath := filepath.Join(destDir, "saved.dat")

	require.NoError(t, b.Save(indexPath, dataPath))

	// Verify files exist
	_, err := os.Stat(indexPath)
	require.NoError(t, err)
	_, err = os.Stat(dataPath)
	require.NoError(t, err)

	// Open and verify
	bf, err := OpenFile(indexPath, dataPath)
	require.NoError(t, err)
	defer bf.Close()

	content, err := bf.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), content)
}

func TestBlobSaveCreatesDirectories(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("hello"),
	}
	b := createTestArchive(t, files, CompressionNone)

	destDir := t.TempDir()
	indexPath := filepath.Join(destDir, "nested", "dir", "saved.idx")
	dataPath := filepath.Join(destDir, "other", "path", "saved.dat")

	require.NoError(t, b.Save(indexPath, dataPath))

	_, err := os.Stat(indexPath)
	require.NoError(t, err)
	_, err = os.Stat(dataPath)
	require.NoError(t, err)
}

func TestCreateBlob(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	destDir := t.TempDir()

	files := map[string]string{
		"a.txt":     "content a",
		"dir/b.txt": "content b",
	}
	createTestFiles(t, srcDir, files)

	bf, err := CreateBlob(context.Background(), srcDir, destDir)
	require.NoError(t, err)
	defer bf.Close()

	// Verify files were created with default names
	_, err = os.Stat(filepath.Join(destDir, DefaultIndexName))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(destDir, DefaultDataName))
	require.NoError(t, err)

	// Verify content
	content, err := bf.ReadFile("a.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("content a"), content)

	content, err = bf.ReadFile("dir/b.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("content b"), content)
}

func TestCreateBlobCustomNames(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	destDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("test"), 0o644))

	bf, err := CreateBlob(context.Background(), srcDir, destDir,
		CreateBlobWithIndexName("custom.idx"),
		CreateBlobWithDataName("custom.dat"),
	)
	require.NoError(t, err)
	defer bf.Close()

	_, err = os.Stat(filepath.Join(destDir, "custom.idx"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(destDir, "custom.dat"))
	require.NoError(t, err)
}

func TestCreateBlobWithCompression(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Large repetitive content that compresses well
	content := bytes.Repeat([]byte("hello "), 100)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "test.txt"), content, 0o644))

	bf, err := CreateBlob(context.Background(), srcDir, destDir,
		CreateBlobWithCompression(CompressionZstd),
	)
	require.NoError(t, err)
	defer bf.Close()

	readContent, err := bf.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, content, readContent)
}

func TestBlobFileClose(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	destDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("test"), 0o644))

	bf, err := CreateBlob(context.Background(), srcDir, destDir)
	require.NoError(t, err)

	// Close should succeed
	require.NoError(t, bf.Close())

	// Second close should also succeed (idempotent)
	require.NoError(t, bf.Close())
}

func TestBlobFileEmbedding(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	destDir := t.TempDir()

	files := map[string]string{
		"a.txt": "content a",
		"b.txt": "content b",
	}
	createTestFiles(t, srcDir, files)

	bf, err := CreateBlob(context.Background(), srcDir, destDir)
	require.NoError(t, err)
	defer bf.Close()

	// Verify Blob methods are accessible through embedding
	assert.Equal(t, 2, bf.Len())

	entries, err := bf.ReadDir(".")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}
