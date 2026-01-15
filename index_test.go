package blob

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/blob/internal/fb"
)

// testEntry holds data for building test index entries.
type testEntry struct {
	Path         string
	DataOffset   uint64
	DataSize     uint64
	OriginalSize uint64
	Hash         []byte
	Mode         fs.FileMode
	UID          uint32
	GID          uint32
	ModTime      time.Time
	Compression  Compression
}

// buildTestIndex creates a FlatBuffers-encoded index from test entries.
// Entries are automatically sorted by path (required for binary search).
func buildTestIndex(tb testing.TB, entries []testEntry) []byte {
	tb.Helper()

	// Sort entries by path (required for EntriesByKey to work)
	slices.SortFunc(entries, func(a, b testEntry) int {
		return strings.Compare(a.Path, b.Path)
	})

	builder := flatbuffers.NewBuilder(1024)

	// Build entries in reverse order (FlatBuffers requirement)
	entryOffsets := make([]flatbuffers.UOffsetT, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]

		// Create path string
		pathOffset := builder.CreateString(e.Path)

		// Create hash vector
		fb.EntryStartHashVector(builder, len(e.Hash))
		for j := len(e.Hash) - 1; j >= 0; j-- {
			builder.PrependByte(e.Hash[j])
		}
		hashOffset := builder.EndVector(len(e.Hash))

		// Build entry
		fb.EntryStart(builder)
		fb.EntryAddPath(builder, pathOffset)
		fb.EntryAddDataOffset(builder, e.DataOffset)
		fb.EntryAddDataSize(builder, e.DataSize)
		fb.EntryAddOriginalSize(builder, e.OriginalSize)
		fb.EntryAddHash(builder, hashOffset)
		fb.EntryAddMode(builder, uint32(e.Mode))
		fb.EntryAddUid(builder, e.UID)
		fb.EntryAddGid(builder, e.GID)
		fb.EntryAddMtimeNs(builder, e.ModTime.UnixNano())
		fb.EntryAddCompression(builder, fb.Compression(e.Compression))
		entryOffsets[i] = fb.EntryEnd(builder)
	}

	// Create entries vector (must be in sorted order for binary search)
	fb.IndexStartEntriesVector(builder, len(entries))
	for i := len(entryOffsets) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(entryOffsets[i])
	}
	entriesOffset := builder.EndVector(len(entries))

	// Build index
	fb.IndexStart(builder)
	fb.IndexAddVersion(builder, 1)
	fb.IndexAddHashAlgorithm(builder, fb.HashAlgorithmSHA256)
	fb.IndexAddEntries(builder, entriesOffset)
	indexOffset := fb.IndexEnd(builder)

	builder.Finish(indexOffset)
	return builder.FinishedBytes()
}

// mustLoadIndex loads an index or fails the test.
// Returns the internal index for testing index-specific behavior.
func mustLoadIndex(tb testing.TB, data []byte) *index {
	tb.Helper()
	idx, err := loadIndex(data)
	require.NoError(tb, err, "loadIndex failed")
	return idx
}

func TestLoadIndex(t *testing.T) {
	t.Parallel()

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		_, err := loadIndex(nil)
		assert.Error(t, err, "expected error for empty data")
	})

	t.Run("valid index", func(t *testing.T) {
		t.Parallel()
		data := buildTestIndex(t, []testEntry{
			{Path: "test.txt", DataOffset: 0, DataSize: 100},
		})
		idx := mustLoadIndex(t, data)
		assert.Equal(t, 1, idx.len())
	})
}

func TestIndexLookup(t *testing.T) {
	t.Parallel()

	entries := []testEntry{
		{Path: "a/file1.txt", DataOffset: 0, DataSize: 100, OriginalSize: 100},
		{Path: "a/file2.txt", DataOffset: 100, DataSize: 200, OriginalSize: 200},
		{Path: "b/file3.txt", DataOffset: 300, DataSize: 150, OriginalSize: 150},
	}
	data := buildTestIndex(t, entries)
	idx := mustLoadIndex(t, data)

	t.Run("existing path", func(t *testing.T) {
		t.Parallel()
		view, ok := idx.lookupView("a/file1.txt")
		require.True(t, ok, "expected to find entry")
		assert.Equal(t, "a/file1.txt", view.Path())
		assert.Equal(t, uint64(0), view.DataOffset())
	})

	t.Run("non-existing path", func(t *testing.T) {
		t.Parallel()
		_, ok := idx.lookupView("nonexistent.txt")
		assert.False(t, ok, "expected not to find entry")
	})

	t.Run("all entries accessible", func(t *testing.T) {
		t.Parallel()
		for _, e := range entries {
			view, ok := idx.lookupView(e.Path)
			require.True(t, ok, "expected to find entry %q", e.Path)
			assert.Equal(t, e.DataOffset, view.DataOffset(), "entry %q offset mismatch", e.Path)
		}
	})
}

func TestIndexEntries(t *testing.T) {
	t.Parallel()

	entries := []testEntry{
		{Path: "c.txt", DataOffset: 200},
		{Path: "a.txt", DataOffset: 0},
		{Path: "b.txt", DataOffset: 100},
	}
	data := buildTestIndex(t, entries) // buildTestIndex sorts them
	idx := mustLoadIndex(t, data)

	expected := []string{"a.txt", "b.txt", "c.txt"}
	paths := make([]string, 0, len(expected))
	for view := range idx.entriesView() {
		paths = append(paths, view.Path())
	}

	assert.Equal(t, expected, paths, "entries should be sorted by path")
}

func TestIndexEntriesWithPrefix(t *testing.T) {
	t.Parallel()

	entries := []testEntry{
		{Path: "assets/css/main.css"},
		{Path: "assets/css/reset.css"},
		{Path: "assets/images/logo.png"},
		{Path: "assets/images/banner.png"},
		{Path: "src/main.go"},
		{Path: "src/util/helper.go"},
	}
	data := buildTestIndex(t, entries)
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
			for view := range idx.entriesWithPrefixView(tc.prefix) {
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

	entries := []testEntry{
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
			Compression:  CompressionZstd,
		},
	}
	data := buildTestIndex(t, entries)
	idx := mustLoadIndex(t, data)

	view, ok := idx.lookupView("test.txt")
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
	assert.Equal(t, CompressionZstd, view.Compression())
}

func TestIndexVersion(t *testing.T) {
	t.Parallel()

	data := buildTestIndex(t, []testEntry{{Path: "test.txt"}})
	idx := mustLoadIndex(t, data)

	assert.Equal(t, uint32(1), idx.version())
}
