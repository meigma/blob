package index

import (
	"crypto/sha256"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/testutil"
)

// mustLoadIndex loads an index or fails the test.
// Returns the internal index for testing index-specific behavior.
func mustLoadIndex(tb testing.TB, data []byte) *Index {
	tb.Helper()
	idx, err := Load(data)
	require.NoError(tb, err, "Load failed")
	return idx
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		_, err := Load(nil)
		assert.Error(t, err, "expected error for empty data")
	})

	t.Run("valid index", func(t *testing.T) {
		t.Parallel()
		data := testutil.BuildTestIndex(t, []testutil.TestEntry{
			{Path: "test.txt", DataOffset: 0, DataSize: 100},
		})
		idx := mustLoadIndex(t, data)
		assert.Equal(t, 1, idx.Len())
	})
}

func TestIndexLookup(t *testing.T) {
	t.Parallel()

	entries := []testutil.TestEntry{
		{Path: "a/file1.txt", DataOffset: 0, DataSize: 100, OriginalSize: 100},
		{Path: "a/file2.txt", DataOffset: 100, DataSize: 200, OriginalSize: 200},
		{Path: "b/file3.txt", DataOffset: 300, DataSize: 150, OriginalSize: 150},
	}
	data := testutil.BuildTestIndex(t, entries)
	idx := mustLoadIndex(t, data)

	t.Run("existing path", func(t *testing.T) {
		t.Parallel()
		view, ok := idx.LookupView("a/file1.txt")
		require.True(t, ok, "expected to find entry")
		assert.Equal(t, "a/file1.txt", view.Path())
		assert.Equal(t, uint64(0), view.DataOffset())
	})

	t.Run("non-existing path", func(t *testing.T) {
		t.Parallel()
		_, ok := idx.LookupView("nonexistent.txt")
		assert.False(t, ok, "expected not to find entry")
	})

	t.Run("all entries accessible", func(t *testing.T) {
		t.Parallel()
		for _, e := range entries {
			view, ok := idx.LookupView(e.Path)
			require.True(t, ok, "expected to find entry %q", e.Path)
			assert.Equal(t, e.DataOffset, view.DataOffset(), "entry %q offset mismatch", e.Path)
		}
	})
}

func TestIndexEntries(t *testing.T) {
	t.Parallel()

	entries := []testutil.TestEntry{
		{Path: "c.txt", DataOffset: 200},
		{Path: "a.txt", DataOffset: 0},
		{Path: "b.txt", DataOffset: 100},
	}
	data := testutil.BuildTestIndex(t, entries) // BuildTestIndex sorts them
	idx := mustLoadIndex(t, data)

	expected := []string{"a.txt", "b.txt", "c.txt"}
	paths := make([]string, 0, len(expected))
	for view := range idx.EntriesView() {
		paths = append(paths, view.Path())
	}

	assert.Equal(t, expected, paths, "entries should be sorted by path")
}

func TestIndexEntriesWithPrefix(t *testing.T) {
	t.Parallel()

	entries := []testutil.TestEntry{
		{Path: "assets/css/main.css"},
		{Path: "assets/css/reset.css"},
		{Path: "assets/images/logo.png"},
		{Path: "assets/images/banner.png"},
		{Path: "src/main.go"},
		{Path: "src/util/helper.go"},
	}
	data := testutil.BuildTestIndex(t, entries)
	idx := mustLoadIndex(t, data)

	tests := []struct {
		name     string
		prefix   string
		expected []string
	}{
		{
			name:     "assets directory",
			prefix:   "assets/",
			expected: []string{"assets/css/main.css", "assets/css/reset.css", "assets/images/banner.png", "assets/images/logo.png"},
		},
		{
			name:     "assets/css subdirectory",
			prefix:   "assets/css/",
			expected: []string{"assets/css/main.css", "assets/css/reset.css"},
		},
		{
			name:     "assets/images subdirectory",
			prefix:   "assets/images/",
			expected: []string{"assets/images/banner.png", "assets/images/logo.png"},
		},
		{
			name:     "src directory",
			prefix:   "src/",
			expected: []string{"src/main.go", "src/util/helper.go"},
		},
		{
			name:     "nonexistent directory",
			prefix:   "nonexistent/",
			expected: []string{},
		},
		{
			name:     "empty prefix matches all",
			prefix:   "",
			expected: []string{"assets/css/main.css", "assets/css/reset.css", "assets/images/banner.png", "assets/images/logo.png", "src/main.go", "src/util/helper.go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			paths := make([]string, 0, len(tc.expected))
			for view := range idx.EntriesWithPrefixView(tc.prefix) {
				paths = append(paths, view.Path())
			}
			assert.Equal(t, tc.expected, paths)
		})
	}
}

func TestIndexEntryMetadata(t *testing.T) {
	t.Parallel()

	modTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	hash := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}

	entries := []testutil.TestEntry{
		{
			Path:         "test.txt",
			DataOffset:   1000,
			DataSize:     500,
			OriginalSize: 1000,
			Hash:         hash,
			Mode:         0o644,
			UID:          1000,
			GID:          1000,
			ModTime:      modTime,
			Compression:  blobtype.CompressionZstd,
		},
	}
	data := testutil.BuildTestIndex(t, entries)
	idx := mustLoadIndex(t, data)

	view, ok := idx.LookupView("test.txt")
	require.True(t, ok, "expected to find entry")

	assert.Equal(t, "test.txt", view.Path())
	assert.Equal(t, uint64(1000), view.DataOffset())
	assert.Equal(t, uint64(500), view.DataSize())
	assert.Equal(t, uint64(1000), view.OriginalSize())
	assert.Equal(t, hash, view.HashBytes())
	assert.Equal(t, fs.FileMode(0o644), view.Mode())
	assert.Equal(t, uint32(1000), view.UID())
	assert.Equal(t, uint32(1000), view.GID())
	assert.True(t, view.ModTime().Equal(modTime), "ModTime mismatch: expected %v, got %v", modTime, view.ModTime())
	assert.Equal(t, blobtype.CompressionZstd, view.Compression())
}

func TestIndexVersion(t *testing.T) {
	t.Parallel()

	data := testutil.BuildTestIndex(t, []testutil.TestEntry{{Path: "test.txt"}})
	idx := mustLoadIndex(t, data)

	assert.Equal(t, uint32(1), idx.Version())
}

func TestIndexDataMetadata(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		data := []byte("data blob bytes")
		hash := sha256.Sum256(data)

		meta := &testutil.IndexMetadata{
			DataSize: uint64(len(data)),
			DataHash: hash[:],
		}

		indexData := testutil.BuildTestIndexWithMetadata(t, []testutil.TestEntry{{Path: "test.txt"}}, meta)
		idx := mustLoadIndex(t, indexData)

		gotHash, ok := idx.DataHash()
		require.True(t, ok, "expected data hash")
		assert.Equal(t, hash[:], gotHash)

		gotSize, ok := idx.DataSize()
		require.True(t, ok, "expected data size")
		assert.Equal(t, meta.DataSize, gotSize)
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		indexData := testutil.BuildTestIndex(t, []testutil.TestEntry{{Path: "test.txt"}})
		idx := mustLoadIndex(t, indexData)

		gotHash, ok := idx.DataHash()
		assert.False(t, ok, "expected no data hash")
		assert.Nil(t, gotHash)

		gotSize, ok := idx.DataSize()
		assert.False(t, ok, "expected no data size")
		assert.Equal(t, uint64(0), gotSize)
	})
}
