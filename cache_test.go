package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/internal/testutil"
)

func TestBlobWithCacheReadFile(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("cached content"),
	}
	b := createTestArchiveWithCache(t, files)

	// First read should populate cache
	content1, err := b.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached content"), content1)

	// Verify cache was populated
	hash := sha256.Sum256([]byte("cached content"))
	cache := b.cache.(*testutil.MockCache)
	cachedContent, ok := cache.GetBytes(hash[:])
	require.True(t, ok, "content should be cached")
	assert.Equal(t, []byte("cached content"), cachedContent)

	// Second read should use cache
	content2, err := b.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached content"), content2)
}

func TestBlobWithCacheOpen(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("cached open content"),
	}
	b := createTestArchiveWithCache(t, files)

	// First open should populate cache
	f1, err := b.Open("test.txt")
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := f1.Read(buf)
	require.NoError(t, err)
	require.NoError(t, f1.Close())
	assert.Equal(t, []byte("cached open content"), buf[:n])

	// Verify cache was populated
	hash := sha256.Sum256([]byte("cached open content"))
	cache := b.cache.(*testutil.MockCache)
	_, ok := cache.GetBytes(hash[:])
	require.True(t, ok, "content should be cached")

	// Second open should use cache
	f2, err := b.Open("test.txt")
	require.NoError(t, err)
	defer f2.Close()

	content, err := readAll(f2)
	require.NoError(t, err)
	assert.Equal(t, []byte("cached open content"), content)
}

func TestBlobWithCacheSingleflight(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("singleflight test content"),
	}
	baseBlob, source := createTestArchiveWithSource(t, files)

	// Wrap source to count reads
	countingSource := &countingByteSource{source: source}

	// Create a new blob with the counting source and cache
	cache := testutil.NewMockCache()
	countingBlob, err := New(baseBlob.IndexData(), countingSource, WithCache(cache))
	require.NoError(t, err)

	// Launch multiple goroutines to read the same file concurrently
	const numGoroutines = 10
	results := make(chan []byte, numGoroutines)
	errors := make(chan error, numGoroutines)

	// Use a barrier to ensure all goroutines start at the same time
	start := make(chan struct{})

	for range numGoroutines {
		go func() {
			<-start // Wait for signal
			content, err := countingBlob.ReadFile("test.txt")
			if err != nil {
				errors <- err
				return
			}
			results <- content
		}()
	}

	// Release all goroutines at once
	close(start)

	// Collect results
	for range numGoroutines {
		select {
		case content := <-results:
			assert.Equal(t, []byte("singleflight test content"), content)
		case err := <-errors:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// With singleflight, we should have exactly 1 read despite 10 concurrent callers
	// (Allow up to 2 in case of race between cache check and singleflight)
	readCount := countingSource.ReadCount()
	assert.LessOrEqual(t, readCount, int64(2), "singleflight should deduplicate concurrent reads (got %d reads)", readCount)
	t.Logf("concurrent reads deduplicated: %d goroutines, %d actual reads", numGoroutines, readCount)
}

func TestBlobWithCacheOpenStat(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("stat test content"),
	}
	b := createTestArchiveWithCache(t, files)

	// Read to populate cache
	_, err := b.ReadFile("test.txt")
	require.NoError(t, err)

	// Open from cache and check Stat
	f, err := b.Open("test.txt")
	require.NoError(t, err)
	defer f.Close()

	info, err := f.Stat()
	require.NoError(t, err)

	assert.Equal(t, "test.txt", info.Name())
	assert.Equal(t, int64(len("stat test content")), info.Size())
	assert.False(t, info.IsDir())
}

func TestBlobWithCacheReadFileHashMismatch(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("cached content"),
	}
	b := createTestArchiveWithCache(t, files)

	hash := sha256.Sum256([]byte("cached content"))
	require.NoError(t, b.cache.Put(hash[:], &bytesFile{
		Reader: bytes.NewReader([]byte("poisoned content")),
		size:   int64(len("poisoned content")),
	}))

	_, err := b.ReadFile("test.txt")
	require.ErrorIs(t, err, ErrHashMismatch)

	cache := b.cache.(*testutil.MockCache)
	_, ok := cache.GetBytes(hash[:])
	require.False(t, ok, "poisoned content should be removed")
}

func TestBlobWithCacheOpenHashMismatch(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("cached open content"),
	}
	b := createTestArchiveWithCache(t, files)

	hash := sha256.Sum256([]byte("cached open content"))
	require.NoError(t, b.cache.Put(hash[:], &bytesFile{
		Reader: bytes.NewReader([]byte("poisoned content")),
		size:   int64(len("poisoned content")),
	}))

	f, err := b.Open("test.txt")
	require.NoError(t, err)
	defer f.Close()

	_, err = readAll(f)
	require.ErrorIs(t, err, ErrHashMismatch)

	cache := b.cache.(*testutil.MockCache)
	_, ok := cache.GetBytes(hash[:])
	require.False(t, ok, "poisoned content should be removed")
}

// createTestArchiveWithCache creates a test archive with caching enabled.
func createTestArchiveWithCache(t *testing.T, files map[string][]byte) *Blob {
	t.Helper()

	var indexBuf, dataBuf bytes.Buffer
	dir := t.TempDir()

	// Write files to temp dir
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, content, 0o644))
	}

	// Create archive
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	// Create blob with cache
	cache := testutil.NewMockCache()
	b, err := New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBuf.Bytes()), WithCache(cache))
	require.NoError(t, err)

	return b
}

// createTestArchiveWithSource creates a test archive and returns both the blob and source.
func createTestArchiveWithSource(t *testing.T, files map[string][]byte) (*Blob, *testutil.MockByteSource) {
	t.Helper()

	var indexBuf, dataBuf bytes.Buffer
	dir := t.TempDir()

	// Write files to temp dir
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, content, 0o644))
	}

	// Create archive
	err := Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	source := testutil.NewMockByteSource(dataBuf.Bytes())

	// Create blob
	b, err := New(indexBuf.Bytes(), source)
	require.NoError(t, err)

	return b, source
}

// countingByteSource wraps a ByteSource and counts ReadAt calls.
type countingByteSource struct {
	source    ByteSource
	readCount atomic.Int64
}

func (c *countingByteSource) ReadAt(p []byte, off int64) (int, error) {
	c.readCount.Add(1)
	return c.source.ReadAt(p, off)
}

func (c *countingByteSource) Size() int64 {
	return c.source.Size()
}

func (c *countingByteSource) ReadCount() int64 {
	return c.readCount.Load()
}

func (c *countingByteSource) SourceID() string {
	return c.source.SourceID()
}

// readAll reads all data from an fs.File.
func readAll(f interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	data := make([]byte, 1024)
	for {
		n, err := f.Read(data)
		if n > 0 {
			buf.Write(data[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}
