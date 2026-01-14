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
)

// mockByteSource implements ByteSource for testing.
type mockByteSource struct {
	data []byte
}

func (m *mockByteSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}
	return n, nil
}

func (m *mockByteSource) Size() int64 {
	return int64(len(m.data))
}

// mockCache implements Cache for testing.
type mockCache struct {
	data map[string][]byte
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string][]byte)}
}

func (c *mockCache) Get(hash []byte) ([]byte, bool) {
	data, ok := c.data[string(hash)]
	return data, ok
}

func (c *mockCache) Put(hash, content []byte) error {
	c.data[string(hash)] = content
	return nil
}

// createTestArchive creates an index and data blob for testing.
// Returns the index and data source.
func createTestArchive(t *testing.T, files map[string][]byte, compression Compression) (*Index, *mockByteSource) {
	t.Helper()

	var indexBuf, dataBuf bytes.Buffer
	dir := t.TempDir()

	// Write files to temp dir
	createTestFilesBytes(t, dir, files)

	// Create archive
	w := NewWriter(WriteOptions{Compression: compression})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	// Load index
	idx, err := LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	return idx, &mockByteSource{data: dataBuf.Bytes()}
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

func TestReaderOpen(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":     []byte("content a"),
		"b.txt":     []byte("content b"),
		"dir/c.txt": []byte("content c"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	t.Run("open file", func(t *testing.T) {
		t.Parallel()
		f, err := r.Open("a.txt")
		require.NoError(t, err)
		defer f.Close()

		info, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, "a.txt", info.Name())
		assert.False(t, info.IsDir())
	})

	t.Run("open directory", func(t *testing.T) {
		t.Parallel()
		f, err := r.Open("dir")
		require.NoError(t, err)
		defer f.Close()

		info, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, "dir", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("open root", func(t *testing.T) {
		t.Parallel()
		f, err := r.Open(".")
		require.NoError(t, err)
		defer f.Close()

		info, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, ".", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("open nonexistent", func(t *testing.T) {
		t.Parallel()
		_, err := r.Open("nonexistent.txt")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("open invalid path", func(t *testing.T) {
		t.Parallel()
		_, err := r.Open("../escape")
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})
}

func TestReaderReadFile(t *testing.T) {
	t.Parallel()

	t.Run("uncompressed", func(t *testing.T) {
		t.Parallel()
		files := map[string][]byte{
			"test.txt": []byte("hello world"),
		}
		idx, source := createTestArchive(t, files, CompressionNone)
		r := NewReader(idx, source)

		content, err := r.ReadFile("test.txt")
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
		idx, source := createTestArchive(t, files, CompressionZstd)
		r := NewReader(idx, source)

		content, err := r.ReadFile("test.txt")
		require.NoError(t, err)
		assert.Equal(t, original, content)
	})

	t.Run("nonexistent", func(t *testing.T) {
		t.Parallel()
		idx, source := createTestArchive(t, map[string][]byte{"a.txt": []byte("a")}, CompressionNone)
		r := NewReader(idx, source)

		_, err := r.ReadFile("nonexistent.txt")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("hash mismatch", func(t *testing.T) {
		t.Parallel()
		files := map[string][]byte{
			"test.txt": []byte("original"),
		}
		idx, source := createTestArchive(t, files, CompressionNone)

		// Corrupt the data
		source.data[0] ^= 0xFF

		r := NewReader(idx, source)
		_, err := r.ReadFile("test.txt")
		assert.ErrorIs(t, err, ErrHashMismatch)
	})
}

func TestReaderStat(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"file.txt":     []byte("content"),
		"dir/file.txt": []byte("nested"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	t.Run("stat file", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("file.txt")
		require.NoError(t, err)
		assert.Equal(t, "file.txt", info.Name())
		assert.Equal(t, int64(7), info.Size())
		assert.False(t, info.IsDir())
	})

	t.Run("stat directory", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("dir")
		require.NoError(t, err)
		assert.Equal(t, "dir", info.Name())
		assert.True(t, info.IsDir())
		assert.Equal(t, fs.ModeDir|0o755, info.Mode())
	})

	t.Run("stat root", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat(".")
		require.NoError(t, err)
		assert.Equal(t, ".", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("stat nonexistent", func(t *testing.T) {
		t.Parallel()
		_, err := r.Stat("nonexistent")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestReaderReadDir(t *testing.T) {
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
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	t.Run("read root", func(t *testing.T) {
		t.Parallel()
		entries, err := r.ReadDir(".")
		require.NoError(t, err)

		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		assert.Equal(t, []string{"a.txt", "b.txt", "dir", "other"}, names)
	})

	t.Run("read subdirectory", func(t *testing.T) {
		t.Parallel()
		entries, err := r.ReadDir("dir")
		require.NoError(t, err)

		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		assert.Equal(t, []string{"c.txt", "d.txt", "sub"}, names)
	})

	t.Run("read nested subdirectory", func(t *testing.T) {
		t.Parallel()
		entries, err := r.ReadDir("other/deep")
		require.NoError(t, err)

		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		assert.Equal(t, []string{"g.txt", "h.txt"}, names)
	})

	t.Run("read nonexistent", func(t *testing.T) {
		t.Parallel()
		_, err := r.ReadDir("nonexistent")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("directory entry types", func(t *testing.T) {
		t.Parallel()
		entries, err := r.ReadDir(".")
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

func TestCachedReaderReadFile(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("cached content"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	cache := newMockCache()
	r := NewCachedReader(NewReader(idx, source), cache)

	// First read should populate cache
	content1, err := r.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached content"), content1)

	// Verify cache was populated
	hash := sha256.Sum256([]byte("cached content"))
	cachedContent, ok := cache.Get(hash[:])
	require.True(t, ok, "content should be cached")
	assert.Equal(t, []byte("cached content"), cachedContent)

	// Corrupt the source - second read should use cache
	source.data[0] ^= 0xFF

	content2, err := r.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached content"), content2)
}

func TestCachedReaderPrefetch(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": []byte("content a"),
		"b.txt": []byte("content b"),
		"c.txt": []byte("content c"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	cache := newMockCache()
	r := NewCachedReader(NewReader(idx, source), cache)

	// Prefetch should populate cache
	err := r.Prefetch("a.txt", "b.txt")
	require.NoError(t, err)

	// Verify cache was populated
	hashA := sha256.Sum256([]byte("content a"))
	hashB := sha256.Sum256([]byte("content b"))
	hashC := sha256.Sum256([]byte("content c"))

	_, okA := cache.Get(hashA[:])
	_, okB := cache.Get(hashB[:])
	_, okC := cache.Get(hashC[:])

	assert.True(t, okA, "a.txt should be cached")
	assert.True(t, okB, "b.txt should be cached")
	assert.False(t, okC, "c.txt should not be cached")
}

func TestCachedReaderPrefetchDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"root.txt":      []byte("root"),
		"dir/a.txt":     []byte("a"),
		"dir/b.txt":     []byte("b"),
		"dir/sub/c.txt": []byte("c"),
		"other/d.txt":   []byte("d"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	cache := newMockCache()
	r := NewCachedReader(NewReader(idx, source), cache)

	// Prefetch dir should populate cache for all files under dir
	err := r.PrefetchDir("dir")
	require.NoError(t, err)

	// Verify cache
	hashRoot := sha256.Sum256([]byte("root"))
	hashA := sha256.Sum256([]byte("a"))
	hashB := sha256.Sum256([]byte("b"))
	hashC := sha256.Sum256([]byte("c"))
	hashD := sha256.Sum256([]byte("d"))

	_, okRoot := cache.Get(hashRoot[:])
	_, okA := cache.Get(hashA[:])
	_, okB := cache.Get(hashB[:])
	_, okC := cache.Get(hashC[:])
	_, okD := cache.Get(hashD[:])

	assert.False(t, okRoot, "root.txt should not be cached")
	assert.True(t, okA, "dir/a.txt should be cached")
	assert.True(t, okB, "dir/b.txt should be cached")
	assert.True(t, okC, "dir/sub/c.txt should be cached")
	assert.False(t, okD, "other/d.txt should not be cached")
}

func TestReaderFSWalk(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":         []byte("a"),
		"dir/b.txt":     []byte("b"),
		"dir/sub/c.txt": []byte("c"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	// Use fs.WalkDir to verify full fs.FS compliance
	var visited []string
	err := fs.WalkDir(r, ".", func(path string, d fs.DirEntry, err error) error {
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

func TestReaderOpenDirReadDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt":     []byte("a"),
		"b.txt":     []byte("b"),
		"dir/c.txt": []byte("c"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	f, err := r.Open(".")
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

func TestReaderOpenDirReadDirN(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": []byte("a"),
		"b.txt": []byte("b"),
		"c.txt": []byte("c"),
		"d.txt": []byte("d"),
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	f, err := r.Open(".")
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

func TestReaderFileRead(t *testing.T) {
	t.Parallel()

	content := []byte("hello world, this is test content")
	files := map[string][]byte{
		"test.txt": content,
	}
	idx, source := createTestArchive(t, files, CompressionNone)
	r := NewReader(idx, source)

	f, err := r.Open("test.txt")
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

func TestReaderCompressedDecompression(t *testing.T) {
	t.Parallel()

	// Create compressed data manually to test decompression path
	original := []byte("test content for compression")
	hash := sha256.Sum256(original)

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressed := enc.EncodeAll(original, nil)
	enc.Close()

	// Build test index manually with compressed entry
	entries := []testEntry{
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
	indexData := buildTestIndex(t, entries)
	idx := mustLoadIndex(t, indexData)

	source := &mockByteSource{data: compressed}
	r := NewReader(idx, source)

	content, err := r.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, original, content)
}
