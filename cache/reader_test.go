package cache

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

	"github.com/meigma/blob"
	"github.com/meigma/blob/internal/testutil"
)

func TestReaderReadFile(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("cached content"),
	}
	idx, source := createTestArchive(t, files)
	cache := testutil.NewMockCache()
	r := NewReader(blob.NewReader(idx, source), cache)

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
	source.Bytes()[0] ^= 0xFF

	content2, err := r.ReadFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached content"), content2)
}

func TestReaderReadFileSingleflight(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"test.txt": []byte("singleflight test content"),
	}
	idx, source := createTestArchive(t, files)

	// Wrap source to count reads
	countingSource := &countingByteSource{source: source}

	cache := testutil.NewMockCache()
	r := NewReader(blob.NewReader(idx, countingSource), cache)

	// Launch multiple goroutines to read the same file concurrently
	const numGoroutines = 10
	results := make(chan []byte, numGoroutines)
	errors := make(chan error, numGoroutines)

	// Use a barrier to ensure all goroutines start at the same time
	start := make(chan struct{})

	for range numGoroutines {
		go func() {
			<-start // Wait for signal
			content, err := r.ReadFile("test.txt")
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

func TestReaderPrefetch(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"a.txt": []byte("content a"),
		"b.txt": []byte("content b"),
		"c.txt": []byte("content c"),
	}
	idx, source := createTestArchive(t, files)
	cache := testutil.NewMockCache()
	r := NewReader(blob.NewReader(idx, source), cache)

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

func TestReaderPrefetchDir(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"root.txt":      []byte("root"),
		"dir/a.txt":     []byte("a"),
		"dir/b.txt":     []byte("b"),
		"dir/sub/c.txt": []byte("c"),
		"other/d.txt":   []byte("d"),
	}
	idx, source := createTestArchive(t, files)
	cache := testutil.NewMockCache()
	r := NewReader(blob.NewReader(idx, source), cache)

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

// createTestArchive creates a test archive from a map of paths to content.
func createTestArchive(t *testing.T, files map[string][]byte) (*blob.Index, *testutil.MockByteSource) {
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
	w := blob.NewWriter(blob.WriteOptions{Compression: blob.CompressionNone})
	err := w.Create(context.Background(), dir, &indexBuf, &dataBuf)
	require.NoError(t, err)

	// Load index
	idx, err := blob.LoadIndex(indexBuf.Bytes())
	require.NoError(t, err)

	return idx, testutil.NewMockByteSource(dataBuf.Bytes())
}

// countingByteSource wraps a ByteSource and counts ReadAt calls.
type countingByteSource struct {
	source    blob.ByteSource
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
