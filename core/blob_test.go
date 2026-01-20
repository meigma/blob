package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/core/testutil"
)

// createTestArchive creates a Blob for testing with the given files and compression.
// Files are specified as a map of path to content.
func createTestArchive(t *testing.T, files map[string][]byte, compression Compression) *Blob {
	t.Helper()

	var indexBuf, dataBuf bytes.Buffer
	dir := t.TempDir()

	// Write files to temp dir
	createTestFilesBytes(t, dir, files)

	// Create archive
	err := Create(context.Background(), dir, &indexBuf, &dataBuf, CreateWithCompression(compression))
	require.NoError(t, err)

	// Create Blob
	b, err := New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBuf.Bytes()))
	require.NoError(t, err)

	return b
}

// createTestFilesBytes creates files in dir from a map of relative path to content bytes.
func createTestFilesBytes(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for path, content := range files {
		createTestFileBytes(t, dir, path, content)
	}
}

// createTestFileBytes creates a single file with the given content.
func createTestFileBytes(t *testing.T, dir, path string, content []byte) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, content, 0o644))
}

func TestBlobOpen(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":     []byte("content a"),
		"b.txt":     []byte("content b"),
		"dir/c.txt": []byte("content c"),
	}
	b := createTestArchive(t, files, CompressionNone)

	t.Run("open file", func(t *testing.T) {
		t.Parallel()
		f, err := b.Open("a.txt")
		require.NoError(t, err)
		defer f.Close()

		info, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, "a.txt", info.Name())
		assert.False(t, info.IsDir())
	})

	t.Run("open directory", func(t *testing.T) {
		t.Parallel()
		f, err := b.Open("dir")
		require.NoError(t, err)
		defer f.Close()

		info, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, "dir", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("open root", func(t *testing.T) {
		t.Parallel()
		f, err := b.Open(".")
		require.NoError(t, err)
		defer f.Close()

		info, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, ".", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("open nonexistent", func(t *testing.T) {
		t.Parallel()
		_, err := b.Open("nonexistent.txt")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("open invalid path", func(t *testing.T) {
		t.Parallel()
		_, err := b.Open("../escape")
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})
}

func TestBlobReadFile(t *testing.T) {
	t.Parallel()

	t.Run("uncompressed", func(t *testing.T) {
		t.Parallel()
		files := map[string][]byte{
			"test.txt": []byte("hello world"),
		}
		b := createTestArchive(t, files, CompressionNone)

		content, err := b.ReadFile("test.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("hello world"), content)
	})

	t.Run("compressed", func(t *testing.T) {
		t.Parallel()
		// Use content that compresses well
		original := bytes.Repeat([]byte("hello "), 100)
		files := map[string][]byte{
			"test.txt": original,
		}
		b := createTestArchive(t, files, CompressionZstd)

		content, err := b.ReadFile("test.txt")
		require.NoError(t, err)
		assert.Equal(t, original, content)
	})

	t.Run("nonexistent", func(t *testing.T) {
		t.Parallel()
		b := createTestArchive(t, map[string][]byte{"a.txt": []byte("a")}, CompressionNone)

		_, err := b.ReadFile("nonexistent.txt")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("hash mismatch", func(t *testing.T) {
		t.Parallel()
		files := map[string][]byte{
			"test.txt": []byte("original"),
		}
		// Create archive manually to get access to source
		var indexBuf, dataBuf bytes.Buffer
		dir := t.TempDir()
		createTestFilesBytes(t, dir, files)
		err := Create(context.Background(), dir, &indexBuf, &dataBuf, CreateWithCompression(CompressionNone))
		require.NoError(t, err)

		// Corrupt the data
		dataBytes := dataBuf.Bytes()
		dataBytes[0] ^= 0xFF

		b, err := New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBytes))
		require.NoError(t, err)
		_, err = b.ReadFile("test.txt")
		assert.ErrorIs(t, err, ErrHashMismatch)
	})
}

func TestBlobStat(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"file.txt":     []byte("content"),
		"dir/file.txt": []byte("nested"),
	}
	b := createTestArchive(t, files, CompressionNone)

	t.Run("stat file", func(t *testing.T) {
		t.Parallel()
		info, err := b.Stat("file.txt")
		require.NoError(t, err)
		assert.Equal(t, "file.txt", info.Name())
		assert.Equal(t, int64(7), info.Size())
		assert.False(t, info.IsDir())
	})

	t.Run("stat directory", func(t *testing.T) {
		t.Parallel()
		info, err := b.Stat("dir")
		require.NoError(t, err)
		assert.Equal(t, "dir", info.Name())
		assert.True(t, info.IsDir())
		assert.Equal(t, fs.ModeDir|0o755, info.Mode())
	})

	t.Run("stat root", func(t *testing.T) {
		t.Parallel()
		info, err := b.Stat(".")
		require.NoError(t, err)
		assert.Equal(t, ".", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("stat nonexistent", func(t *testing.T) {
		t.Parallel()
		_, err := b.Stat("nonexistent")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestBlobReadDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":             []byte("a"),
		"b.txt":             []byte("b"),
		"dir/c.txt":         []byte("c"),
		"dir/d.txt":         []byte("d"),
		"dir/sub/e.txt":     []byte("e"),
		"other/f.txt":       []byte("f"),
		"other/deep/g.txt":  []byte("g"),
		"other/deep/h.txt":  []byte("h"),
		"other/deep2/i.txt": []byte("i"),
	}
	b := createTestArchive(t, files, CompressionNone)

	t.Run("read root", func(t *testing.T) {
		t.Parallel()
		entries, err := b.ReadDir(".")
		require.NoError(t, err)

		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		assert.Equal(t, []string{"a.txt", "b.txt", "dir", "other"}, names)
	})

	t.Run("read subdirectory", func(t *testing.T) {
		t.Parallel()
		entries, err := b.ReadDir("dir")
		require.NoError(t, err)

		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		assert.Equal(t, []string{"c.txt", "d.txt", "sub"}, names)
	})

	t.Run("read nested subdirectory", func(t *testing.T) {
		t.Parallel()
		entries, err := b.ReadDir("other/deep")
		require.NoError(t, err)

		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		assert.Equal(t, []string{"g.txt", "h.txt"}, names)
	})

	t.Run("read nonexistent", func(t *testing.T) {
		t.Parallel()
		_, err := b.ReadDir("nonexistent")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("directory entry types", func(t *testing.T) {
		t.Parallel()
		entries, err := b.ReadDir(".")
		require.NoError(t, err)

		for _, e := range entries {
			if e.Name() == "a.txt" || e.Name() == "b.txt" {
				assert.False(t, e.IsDir(), "file should not be dir")
				assert.Equal(t, fs.FileMode(0), e.Type())
			} else {
				assert.True(t, e.IsDir(), "directory should be dir")
				assert.Equal(t, fs.ModeDir, e.Type())
			}
		}
	})
}

func TestBlobFSWalk(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":         []byte("a"),
		"dir/b.txt":     []byte("b"),
		"dir/sub/c.txt": []byte("c"),
	}
	b := createTestArchive(t, files, CompressionNone)

	// Use fs.WalkDir to verify full fs.FS compliance
	var visited []string
	err := fs.WalkDir(b, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, path)
		return nil
	})
	require.NoError(t, err)

	expected := []string{".", "a.txt", "dir", "dir/b.txt", "dir/sub", "dir/sub/c.txt"}
	assert.Equal(t, expected, visited)
}

func TestBlobOpenDirReadDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":     []byte("a"),
		"b.txt":     []byte("b"),
		"dir/c.txt": []byte("c"),
	}
	b := createTestArchive(t, files, CompressionNone)

	f, err := b.Open(".")
	require.NoError(t, err)
	defer f.Close()

	// Cast to ReadDirFile
	rdf, ok := f.(fs.ReadDirFile)
	require.True(t, ok, "opened directory should implement ReadDirFile")

	// Read all entries
	entries, err := rdf.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Read again should return empty (all consumed)
	entries2, err := rdf.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, entries2, 0)
}

func TestBlobOpenDirReadDirN(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": []byte("a"),
		"b.txt": []byte("b"),
		"c.txt": []byte("c"),
		"d.txt": []byte("d"),
	}
	b := createTestArchive(t, files, CompressionNone)

	f, err := b.Open(".")
	require.NoError(t, err)
	defer f.Close()

	rdf := f.(fs.ReadDirFile)

	// Read 2 at a time
	entries1, err := rdf.ReadDir(2)
	require.NoError(t, err)
	assert.Len(t, entries1, 2)

	entries2, err := rdf.ReadDir(2)
	require.NoError(t, err)
	assert.Len(t, entries2, 2)

	// Should return EOF when exhausted
	_, err = rdf.ReadDir(2)
	assert.ErrorIs(t, err, io.EOF)
}

func TestBlobFileRead(t *testing.T) {
	t.Parallel()

	content := []byte("hello world, this is test content")
	files := map[string][]byte{
		"test.txt": content,
	}
	b := createTestArchive(t, files, CompressionNone)

	f, err := b.Open("test.txt")
	require.NoError(t, err)
	defer f.Close()

	// Read in chunks
	buf := make([]byte, 10)
	var result []byte
	for {
		n, err := f.Read(buf)
		result = append(result, buf[:n]...)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	assert.Equal(t, content, result)
}

func TestBlobCompressedDecompression(t *testing.T) {
	t.Parallel()

	// Create compressed data manually to test decompression path
	original := []byte("test content for compression")
	hash := sha256.Sum256(original)

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressed := enc.EncodeAll(original, nil)
	enc.Close()

	// Build test index manually with compressed entry
	entries := []testutil.TestEntry{
		{
			Path:         "test.txt",
			DataOffset:   0,
			DataSize:     uint64(len(compressed)),
			OriginalSize: uint64(len(original)),
			Hash:         hash[:],
			Mode:         0o644,
			Compression:  CompressionZstd,
		},
	}
	indexData := testutil.BuildTestIndex(t, entries)

	source := testutil.NewMockByteSource(compressed)
	b, err := New(indexData, source)
	require.NoError(t, err)

	content, err := b.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, original, content)
}
