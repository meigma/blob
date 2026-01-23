package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/core/testutil"
)

func TestCopyDirRejectsTraversalPaths(t *testing.T) {
	data := []byte("pwned")
	hash := sha256.Sum256(data)
	indexData := testutil.BuildTestIndexWithMetadata(t, []testutil.TestEntry{
		{
			Path:         "../pwned.txt",
			DataOffset:   0,
			DataSize:     uint64(len(data)),
			OriginalSize: uint64(len(data)),
			Hash:         hash[:],
			Mode:         0o644,
		},
	}, &testutil.IndexMetadata{
		DataSize: uint64(len(data)),
		DataHash: hash[:],
	})

	archive, err := New(indexData, testutil.NewMockByteSource(data))
	require.NoError(t, err)

	destDir := t.TempDir()
	_, err = archive.CopyDir(destDir, "")
	var pathErr *fs.PathError
	require.ErrorAs(t, err, &pathErr)
	require.ErrorIs(t, pathErr.Err, fs.ErrInvalid)
	_, statErr := os.Stat(filepath.Join(destDir, "..", "pwned.txt"))
	require.Error(t, statErr)
}

func TestCopyDir_ReturnsStats(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":     bytes.Repeat([]byte("a"), 100),
		"b.txt":     bytes.Repeat([]byte("b"), 200),
		"dir/c.txt": bytes.Repeat([]byte("c"), 300),
	}
	b := createTestArchive(t, files, CompressionNone)

	destDir := t.TempDir()
	stats, err := b.CopyDir(destDir, "")
	require.NoError(t, err)

	assert.Equal(t, 3, stats.FileCount)
	assert.Equal(t, uint64(600), stats.TotalBytes)
	assert.Equal(t, 0, stats.Skipped)
}

func TestCopyDir_SkippedStats(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": []byte("aaa"),
		"b.txt": []byte("bbb"),
	}
	b := createTestArchive(t, files, CompressionNone)

	destDir := t.TempDir()

	// Pre-create one file
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "a.txt"), []byte("existing"), 0o644))

	// Without overwrite, a.txt should be skipped
	stats, err := b.CopyDir(destDir, "")
	require.NoError(t, err)

	assert.Equal(t, 1, stats.FileCount)          // Only b.txt copied
	assert.Equal(t, uint64(3), stats.TotalBytes) // Only b.txt size
	assert.Equal(t, 1, stats.Skipped)            // a.txt skipped
}

func TestCopyTo_ReturnsStats(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": bytes.Repeat([]byte("a"), 100),
		"b.txt": bytes.Repeat([]byte("b"), 200),
		"c.txt": bytes.Repeat([]byte("c"), 300),
	}
	b := createTestArchive(t, files, CompressionNone)

	destDir := t.TempDir()
	stats, err := b.CopyTo(destDir, "a.txt", "b.txt")
	require.NoError(t, err)

	assert.Equal(t, 2, stats.FileCount)
	assert.Equal(t, uint64(300), stats.TotalBytes)
	assert.Equal(t, 0, stats.Skipped)
}

func TestBlob_Exists(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"etc/nginx/nginx.conf": []byte("config"),
		"etc/hosts":            []byte("hosts"),
	}
	b := createTestArchive(t, files, CompressionNone)

	tests := []struct {
		path string
		want bool
	}{
		{"etc/nginx/nginx.conf", true},
		{"etc/hosts", true},
		{"etc/nginx", true},
		{"etc", true},
		{".", true},
		{"/etc/nginx", true},
		{"etc/nginx/", true},
		{"/etc/nginx/", true},
		{"nonexistent", false},
		{"etc/nginx/nonexistent", false},
		// Invalid paths (after normalization) return false
		{"../escape", false},
		{"etc/../hosts", false},
		{"etc/./nginx", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := b.Exists(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBlob_IsDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"etc/nginx/nginx.conf": []byte("config"),
		"etc/hosts":            []byte("hosts"),
	}
	b := createTestArchive(t, files, CompressionNone)

	tests := []struct {
		path string
		want bool
	}{
		{"etc/nginx", true},
		{"etc", true},
		{".", true},
		{"/etc/nginx/", true},
		{"etc/nginx/nginx.conf", false},
		{"etc/hosts", false},
		{"nonexistent", false},
		// Invalid paths return false
		{"../escape", false},
		{"etc/../hosts", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := b.IsDir(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBlob_IsFile(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"etc/nginx/nginx.conf": []byte("config"),
		"etc/hosts":            []byte("hosts"),
	}
	b := createTestArchive(t, files, CompressionNone)

	tests := []struct {
		path string
		want bool
	}{
		{"etc/nginx/nginx.conf", true},
		{"etc/hosts", true},
		{"/etc/hosts", true},
		{"etc/nginx", false},
		{"etc", false},
		{".", false},
		{"nonexistent", false},
		// Invalid paths return false
		{"../escape", false},
		{"etc/../hosts", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := b.IsFile(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

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

func TestBlob_CopyFile(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")
	files := map[string][]byte{
		"config/app.json": content,
		"dir/nested.txt":  []byte("nested"),
	}
	b := createTestArchive(t, files, CompressionNone)

	t.Run("copy with rename", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "renamed.json")

		stats, err := b.CopyFile("config/app.json", dest)
		require.NoError(t, err)

		// Verify stats
		assert.Equal(t, 1, stats.FileCount)
		assert.Equal(t, uint64(len(content)), stats.TotalBytes)
		assert.Equal(t, 0, stats.Skipped)

		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("skip existing without overwrite", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "existing.json")
		require.NoError(t, os.WriteFile(dest, []byte("existing"), 0o644))

		_, err := b.CopyFile("config/app.json", dest)
		assert.ErrorIs(t, err, fs.ErrExist)
	})

	t.Run("overwrite existing", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "overwrite.json")
		require.NoError(t, os.WriteFile(dest, []byte("old"), 0o644))

		_, err := b.CopyFile("config/app.json", dest, CopyWithOverwrite(true))
		require.NoError(t, err)

		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("error on directory", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "out.txt")

		// "config" is a synthetic directory (has config/app.json under it).
		// Since directories aren't stored as entries, this returns ErrNotExist.
		_, err := b.CopyFile("config", dest)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("error on not found", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "out.txt")

		_, err := b.CopyFile("nonexistent.txt", dest)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("error when parent missing", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "missing", "parent", "file.txt")

		_, err := b.CopyFile("config/app.json", dest)
		assert.Error(t, err)
	})

	t.Run("normalizes input path", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "normalized.json")

		// Leading slash should be stripped
		_, err := b.CopyFile("/config/app.json", dest)
		require.NoError(t, err)

		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("rejects CopyWithCleanDest", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "test.json")

		_, err := b.CopyFile("config/app.json", dest, CopyWithCleanDest(true))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CopyWithCleanDest")
	})

	t.Run("refuses to overwrite directory", func(t *testing.T) {
		t.Parallel()
		destDir := t.TempDir()
		dest := filepath.Join(destDir, "target")
		// Create a directory at the destination
		require.NoError(t, os.Mkdir(dest, 0o755))

		_, err := b.CopyFile("config/app.json", dest, CopyWithOverwrite(true))
		var pathErr *fs.PathError
		require.ErrorAs(t, err, &pathErr)
		assert.Equal(t, "copyfile", pathErr.Op)
		assert.Equal(t, dest, pathErr.Path)
		assert.Contains(t, pathErr.Err.Error(), "directory")
	})
}

func TestBlob_CopyFile_Compressed(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte("compress me "), 100)
	files := map[string][]byte{
		"data.bin": content,
	}
	b := createTestArchive(t, files, CompressionZstd)

	dest := filepath.Join(t.TempDir(), "extracted.bin")
	_, err := b.CopyFile("data.bin", dest)
	require.NoError(t, err)

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestBlob_CopyFile_PreserveMode(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not preserved on Windows")
	}

	// Create archive with specific file mode
	content := []byte("executable content")
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "script.sh")
	require.NoError(t, os.WriteFile(srcPath, content, 0o644))
	// Explicitly set mode to avoid umask interference
	require.NoError(t, os.Chmod(srcPath, 0o755))

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf, CreateWithCompression(CompressionNone))
	require.NoError(t, err)

	b, err := New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBuf.Bytes()))
	require.NoError(t, err)

	t.Run("without preserve mode uses default", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "script.sh")

		_, err := b.CopyFile("script.sh", dest)
		require.NoError(t, err)

		info, err := os.Stat(dest)
		require.NoError(t, err)
		// Without preserve mode, should NOT have execute bits (uses umask default)
		// The exact mode depends on umask, but it shouldn't be 0755
		assert.NotEqual(t, fs.FileMode(0o755), info.Mode().Perm())
	})

	t.Run("with preserve mode keeps original", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "script.sh")

		_, err := b.CopyFile("script.sh", dest, CopyWithPreserveMode(true))
		require.NoError(t, err)

		info, err := os.Stat(dest)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o755), info.Mode().Perm())
	})
}

func TestBlob_CopyFile_PreserveTimes(t *testing.T) {
	t.Parallel()

	content := []byte("timed content")
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(srcPath, content, 0o644))

	// Set a specific modification time in the past
	pastTime := time.Date(2020, 1, 15, 10, 30, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(srcPath, pastTime, pastTime))

	var indexBuf, dataBuf bytes.Buffer
	err := Create(context.Background(), dir, &indexBuf, &dataBuf, CreateWithCompression(CompressionNone))
	require.NoError(t, err)

	b, err := New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBuf.Bytes()))
	require.NoError(t, err)

	t.Run("without preserve times uses current time", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "file.txt")
		// Use a 2-second buffer to handle coarse filesystem timestamp resolution
		beforeCopy := time.Now().Add(-2 * time.Second)

		_, err := b.CopyFile("file.txt", dest)
		require.NoError(t, err)

		info, err := os.Stat(dest)
		require.NoError(t, err)
		// Without preserve times, mod time should be recent (at or after beforeCopy)
		assert.False(t, info.ModTime().Before(beforeCopy), "mod time should be recent")
	})

	t.Run("with preserve times keeps original", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "file.txt")

		_, err := b.CopyFile("file.txt", dest, CopyWithPreserveTimes(true))
		require.NoError(t, err)

		info, err := os.Stat(dest)
		require.NoError(t, err)
		// With preserve times, should match the original time
		// Allow 1 second tolerance for filesystem precision
		diff := info.ModTime().Sub(pastTime)
		if diff < 0 {
			diff = -diff
		}
		assert.Less(t, diff, time.Second, "mod time should match original")
	})
}

func TestBlob_DirStats(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":                bytes.Repeat([]byte("a"), 100),
		"b.txt":                bytes.Repeat([]byte("b"), 200),
		"etc/nginx/nginx.conf": bytes.Repeat([]byte("n"), 300),
		"etc/nginx/mime.types": bytes.Repeat([]byte("m"), 150),
		"etc/hosts":            bytes.Repeat([]byte("h"), 50),
	}
	b := createTestArchive(t, files, CompressionNone)

	tests := []struct {
		name      string
		prefix    string
		wantCount int
		wantBytes uint64
	}{
		{name: "entire archive with dot", prefix: ".", wantCount: 5, wantBytes: 800},
		{name: "entire archive with empty", prefix: "", wantCount: 5, wantBytes: 800},
		{name: "subdirectory", prefix: "etc/nginx", wantCount: 2, wantBytes: 450},
		{name: "parent directory", prefix: "etc", wantCount: 3, wantBytes: 500},
		{name: "nonexistent prefix", prefix: "nonexistent", wantCount: 0, wantBytes: 0},
		{name: "normalized leading slash", prefix: "/etc/nginx", wantCount: 2, wantBytes: 450},
		{name: "normalized trailing slash", prefix: "etc/nginx/", wantCount: 2, wantBytes: 450},
		// Exact file match cases
		{name: "exact file match", prefix: "etc/hosts", wantCount: 1, wantBytes: 50},
		{name: "exact file match nested", prefix: "etc/nginx/nginx.conf", wantCount: 1, wantBytes: 300},
		{name: "exact file match with leading slash", prefix: "/a.txt", wantCount: 1, wantBytes: 100},
		// Invalid path cases
		{name: "path traversal", prefix: "../escape", wantCount: 0, wantBytes: 0},
		{name: "absolute path traversal", prefix: "/../etc", wantCount: 0, wantBytes: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stats := b.DirStats(tt.prefix)
			assert.Equal(t, tt.wantCount, stats.FileCount)
			assert.Equal(t, tt.wantBytes, stats.TotalBytes)
		})
	}
}

func TestBlob_DirStats_Compressed(t *testing.T) {
	t.Parallel()

	// Create compressible content
	files := map[string][]byte{
		"data.txt": bytes.Repeat([]byte("compressible data "), 100),
	}
	b := createTestArchive(t, files, CompressionZstd)

	stats := b.DirStats(".")
	assert.Equal(t, 1, stats.FileCount)
	assert.Equal(t, uint64(1800), stats.TotalBytes)         // Original size
	assert.Less(t, stats.CompressedBytes, stats.TotalBytes) // Compressed should be smaller
}

func TestBlob_ValidateFiles(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"file1.txt":    []byte("content1"),
		"file2.txt":    []byte("content2"),
		"dir/file.txt": []byte("nested"),
	}
	b := createTestArchive(t, files, CompressionNone)

	t.Run("all valid", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles("file1.txt", "file2.txt")
		require.NoError(t, err)
		assert.Equal(t, []string{"file1.txt", "file2.txt"}, normalized)
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles()
		require.NoError(t, err)
		assert.Empty(t, normalized)
	})

	t.Run("single valid file", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles("file1.txt")
		require.NoError(t, err)
		assert.Equal(t, []string{"file1.txt"}, normalized)
	})

	t.Run("returns normalized paths", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles("/file1.txt", "dir/file.txt/")
		require.NoError(t, err)
		assert.Equal(t, []string{"file1.txt", "dir/file.txt"}, normalized)

		// Verify normalized paths work with Open
		for _, p := range normalized {
			f, err := b.Open(p)
			require.NoError(t, err)
			f.Close()
		}
	})

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles("file1.txt", "nonexistent.txt", "file2.txt")
		require.Error(t, err)
		assert.Nil(t, normalized)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "nonexistent.txt", valErr.Path)
		assert.Equal(t, "not found", valErr.Reason)
		assert.Contains(t, err.Error(), "nonexistent.txt")
	})

	t.Run("directory not allowed", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles("dir")
		require.Error(t, err)
		assert.Nil(t, normalized)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "dir", valErr.Path)
		assert.Equal(t, "is a directory", valErr.Reason)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("invalid path", func(t *testing.T) {
		t.Parallel()
		normalized, err := b.ValidateFiles("../escape")
		require.Error(t, err)
		assert.Nil(t, normalized)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "../escape", valErr.Path)
		assert.Equal(t, "invalid path", valErr.Reason)
	})

	t.Run("preserves original path in error", func(t *testing.T) {
		t.Parallel()
		// Even though path is normalized, error should show original path
		normalized, err := b.ValidateFiles("/nonexistent.txt")
		require.Error(t, err)
		assert.Nil(t, normalized)

		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		assert.Equal(t, "/nonexistent.txt", valErr.Path)
	})
}
