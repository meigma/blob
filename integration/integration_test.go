//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memByteSource provides an in-memory implementation of blob.ByteSource.
type memByteSource struct {
	data []byte
}

func (m *memByteSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}
	return n, nil
}

func (m *memByteSource) Size() int64 {
	return int64(len(m.data))
}

// createFiles writes test files to a directory.
func createFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, content, 0o644))
	}
}

// createArchive builds an archive from the given files and returns the index and data source.
func createArchive(t *testing.T, files map[string][]byte, compression blob.Compression) (*blob.Index, *memByteSource) {
	t.Helper()

	dir := t.TempDir()
	createFiles(t, dir, files)

	var indexBuf, dataBuf bytes.Buffer
	w := blob.NewWriter(blob.WriteOptions{Compression: compression})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	idx, err := blob.LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	return idx, &memByteSource{data: dataBuf.Bytes()}
}

func TestE2E_WriteAndRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string][]byte
		compression blob.Compression
	}{
		{
			name: "single file",
			files: map[string][]byte{
				"hello.txt": []byte("Hello, World!"),
			},
			compression: blob.CompressionNone,
		},
		{
			name: "multiple files flat",
			files: map[string][]byte{
				"a.txt": []byte("content a"),
				"b.txt": []byte("content b"),
				"c.txt": []byte("content c"),
			},
			compression: blob.CompressionNone,
		},
		{
			name: "nested directories",
			files: map[string][]byte{
				"root.txt":          []byte("root file"),
				"a/file.txt":        []byte("level 1"),
				"a/b/file.txt":      []byte("level 2"),
				"a/b/c/file.txt":    []byte("level 3"),
				"a/b/c/d/file.txt":  []byte("level 4"),
				"other/sibling.txt": []byte("sibling directory"),
			},
			compression: blob.CompressionNone,
		},
		{
			name: "with zstd compression",
			files: map[string][]byte{
				"compressible.txt": bytes.Repeat([]byte("compress me "), 1000),
				"small.txt":        []byte("tiny"),
			},
			compression: blob.CompressionZstd,
		},
		{
			name: "large file",
			files: map[string][]byte{
				"large.bin": makeLargeContent(1024 * 1024), // 1MB
			},
			compression: blob.CompressionZstd,
		},
		{
			name: "binary content",
			files: map[string][]byte{
				"random.bin": makeRandomBytes(4096),
			},
			compression: blob.CompressionNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idx, source := createArchive(t, tc.files, tc.compression)
			r := blob.NewReader(idx, source)

			// Verify each file can be read back correctly
			for path, expectedContent := range tc.files {
				path := filepath.ToSlash(path)

				// ReadFile
				gotContent, err := r.ReadFile(path)
				require.NoError(t, err, "ReadFile(%q)", path)
				assert.Equal(t, expectedContent, gotContent, "content mismatch for %q", path)

				// Stat
				info, err := r.Stat(path)
				require.NoError(t, err, "Stat(%q)", path)
				assert.Equal(t, int64(len(expectedContent)), info.Size(), "size mismatch for %q", path)
				assert.False(t, info.IsDir(), "expected file, not directory for %q", path)
			}
		})
	}
}

func TestE2E_DirectoryListing(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"root.txt":       []byte("root"),
		"dir1/a.txt":     []byte("a"),
		"dir1/b.txt":     []byte("b"),
		"dir1/sub/c.txt": []byte("c"),
		"dir2/x.txt":     []byte("x"),
	}

	idx, source := createArchive(t, files, blob.CompressionNone)
	r := blob.NewReader(idx, source)

	t.Run("root directory", func(t *testing.T) {
		entries, err := r.ReadDir(".")
		require.NoError(t, err)

		names := extractNames(entries)
		assert.ElementsMatch(t, []string{"root.txt", "dir1", "dir2"}, names)
	})

	t.Run("nested directory", func(t *testing.T) {
		entries, err := r.ReadDir("dir1")
		require.NoError(t, err)

		names := extractNames(entries)
		assert.ElementsMatch(t, []string{"a.txt", "b.txt", "sub"}, names)
	})

	t.Run("deeply nested directory", func(t *testing.T) {
		entries, err := r.ReadDir("dir1/sub")
		require.NoError(t, err)

		names := extractNames(entries)
		assert.ElementsMatch(t, []string{"c.txt"}, names)
	})

	t.Run("directory entry types", func(t *testing.T) {
		entries, err := r.ReadDir(".")
		require.NoError(t, err)

		for _, e := range entries {
			if e.Name() == "root.txt" {
				assert.False(t, e.IsDir(), "root.txt should be a file")
			} else {
				assert.True(t, e.IsDir(), "%s should be a directory", e.Name())
			}
		}
	})
}

func TestE2E_OpenAndStream(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte("stream test data "), 1000)
	files := map[string][]byte{
		"stream.txt": content,
	}

	idx, source := createArchive(t, files, blob.CompressionZstd)
	r := blob.NewReader(idx, source)

	f, err := r.Open("stream.txt")
	require.NoError(t, err)
	defer f.Close()

	// Read in chunks to simulate streaming
	var buf bytes.Buffer
	chunk := make([]byte, 1024)
	for {
		n, err := f.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	assert.Equal(t, content, buf.Bytes())
}

// extractNames returns the names from directory entries.
func extractNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// makeLargeContent creates compressible content of the specified size.
func makeLargeContent(size int) []byte {
	pattern := []byte("This is a repeating pattern for compression testing. ")
	result := make([]byte, 0, size)
	for len(result) < size {
		result = append(result, pattern...)
	}
	return result[:size]
}

// makeRandomBytes creates random binary content.
func makeRandomBytes(size int) []byte {
	data := make([]byte, size)
	_, _ = rand.Read(data)
	return data
}

func TestE2E_CopyTo(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":         []byte("content a"),
		"b.txt":         []byte("content b"),
		"dir/c.txt":     []byte("content c"),
		"dir/sub/d.txt": []byte("content d"),
	}

	idx, source := createArchive(t, files, blob.CompressionZstd)
	r := blob.NewReader(idx, source)

	t.Run("copy specific files", func(t *testing.T) {
		destDir := t.TempDir()

		err := r.CopyTo(destDir, "a.txt", "dir/c.txt")
		require.NoError(t, err)

		// Verify extracted files
		content, err := os.ReadFile(filepath.Join(destDir, "a.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("content a"), content)

		content, err = os.ReadFile(filepath.Join(destDir, "dir/c.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("content c"), content)

		// Files not requested should not exist
		_, err = os.Stat(filepath.Join(destDir, "b.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("skip existing files", func(t *testing.T) {
		destDir := t.TempDir()

		// Pre-create file with different content
		existing := filepath.Join(destDir, "a.txt")
		require.NoError(t, os.WriteFile(existing, []byte("original"), 0o644))

		err := r.CopyTo(destDir, "a.txt")
		require.NoError(t, err)

		// File should not be overwritten
		content, err := os.ReadFile(existing)
		require.NoError(t, err)
		assert.Equal(t, []byte("original"), content)
	})

	t.Run("overwrite existing files", func(t *testing.T) {
		destDir := t.TempDir()

		// Pre-create file with different content
		existing := filepath.Join(destDir, "a.txt")
		require.NoError(t, os.WriteFile(existing, []byte("original"), 0o644))

		err := r.CopyToWithOptions(destDir, []string{"a.txt"}, blob.CopyWithOverwrite(true))
		require.NoError(t, err)

		// File should be overwritten
		content, err := os.ReadFile(existing)
		require.NoError(t, err)
		assert.Equal(t, []byte("content a"), content)
	})
}

func TestE2E_CopyDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"root.txt":      []byte("root"),
		"dir/a.txt":     []byte("a"),
		"dir/b.txt":     []byte("b"),
		"dir/sub/c.txt": []byte("c"),
		"other/x.txt":   []byte("x"),
	}

	idx, source := createArchive(t, files, blob.CompressionZstd)
	r := blob.NewReader(idx, source)

	t.Run("copy entire directory", func(t *testing.T) {
		destDir := t.TempDir()

		err := r.CopyDir(destDir, "dir")
		require.NoError(t, err)

		// Verify extracted files
		content, err := os.ReadFile(filepath.Join(destDir, "dir/a.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("a"), content)

		content, err = os.ReadFile(filepath.Join(destDir, "dir/b.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("b"), content)

		content, err = os.ReadFile(filepath.Join(destDir, "dir/sub/c.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("c"), content)

		// Files outside prefix should not exist
		_, err = os.Stat(filepath.Join(destDir, "root.txt"))
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(destDir, "other/x.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("copy all files with empty prefix", func(t *testing.T) {
		destDir := t.TempDir()

		err := r.CopyDir(destDir, "")
		require.NoError(t, err)

		// All files should be extracted
		for path, expectedContent := range files {
			content, err := os.ReadFile(filepath.Join(destDir, path))
			require.NoError(t, err, "reading %s", path)
			assert.Equal(t, expectedContent, content, "content mismatch for %s", path)
		}
	})
}
