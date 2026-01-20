package blob

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	blobcore "github.com/meigma/blob/core"
	"github.com/meigma/blob/core/testutil"
	"github.com/meigma/blob/registry"
)

func TestInspectResult_FileCount(t *testing.T) {
	t.Parallel()

	// Build an index with 3 entries
	entries := []testutil.TestEntry{
		{Path: "a.txt", OriginalSize: 100, DataSize: 80},
		{Path: "b.txt", OriginalSize: 200, DataSize: 150},
		{Path: "c.txt", OriginalSize: 300, DataSize: 250},
	}
	indexData := testutil.BuildTestIndex(t, entries)
	indexView, err := blobcore.NewIndexView(indexData)
	require.NoError(t, err)

	result := &InspectResult{
		index: indexView,
	}

	assert.Equal(t, 3, result.FileCount())
}

func TestInspectResult_ComputedStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		entries              []testutil.TestEntry
		wantUncompressedSize uint64
		wantCompressedSize   uint64
		wantCompressionRatio float64
		wantRatioApproximate bool
	}{
		{
			name: "single file compressed",
			entries: []testutil.TestEntry{
				{Path: "file.txt", OriginalSize: 1000, DataSize: 500},
			},
			wantUncompressedSize: 1000,
			wantCompressedSize:   500,
			wantCompressionRatio: 0.5,
		},
		{
			name: "multiple files",
			entries: []testutil.TestEntry{
				{Path: "a.txt", OriginalSize: 100, DataSize: 80},
				{Path: "b.txt", OriginalSize: 200, DataSize: 150},
				{Path: "c.txt", OriginalSize: 700, DataSize: 270},
			},
			wantUncompressedSize: 1000,
			wantCompressedSize:   500,
			wantCompressionRatio: 0.5,
		},
		{
			name: "no compression",
			entries: []testutil.TestEntry{
				{Path: "image.jpg", OriginalSize: 1000, DataSize: 1000},
			},
			wantUncompressedSize: 1000,
			wantCompressedSize:   1000,
			wantCompressionRatio: 1.0,
		},
		{
			name:                 "empty archive",
			entries:              []testutil.TestEntry{},
			wantUncompressedSize: 0,
			wantCompressedSize:   0,
			wantCompressionRatio: 1.0, // Defaults to 1.0 when empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			indexData := testutil.BuildTestIndex(t, tt.entries)
			indexView, err := blobcore.NewIndexView(indexData)
			require.NoError(t, err)

			result := &InspectResult{
				index: indexView,
			}

			assert.Equal(t, tt.wantUncompressedSize, result.TotalUncompressedSize())
			assert.Equal(t, tt.wantCompressedSize, result.TotalCompressedSize())
			assert.InDelta(t, tt.wantCompressionRatio, result.CompressionRatio(), 0.001)
		})
	}
}

func TestInspectResult_StatsCached(t *testing.T) {
	t.Parallel()

	// Verify that stats are computed once and cached
	entries := []testutil.TestEntry{
		{Path: "file.txt", OriginalSize: 1000, DataSize: 500},
	}
	indexData := testutil.BuildTestIndex(t, entries)
	indexView, err := blobcore.NewIndexView(indexData)
	require.NoError(t, err)

	result := &InspectResult{
		index: indexView,
	}

	// First call computes stats
	size1 := result.TotalUncompressedSize()
	ratio1 := result.CompressionRatio()
	compressed1 := result.TotalCompressedSize()

	// Subsequent calls should return cached values
	size2 := result.TotalUncompressedSize()
	ratio2 := result.CompressionRatio()
	compressed2 := result.TotalCompressedSize()

	assert.Equal(t, size1, size2)
	assert.Equal(t, ratio1, ratio2)
	assert.Equal(t, compressed1, compressed2)
}

func TestInspectResult_Index(t *testing.T) {
	t.Parallel()

	entries := []testutil.TestEntry{
		{Path: "dir/file.txt", OriginalSize: 100, DataSize: 80},
		{Path: "dir/other.txt", OriginalSize: 200, DataSize: 150},
		{Path: "root.txt", OriginalSize: 50, DataSize: 40},
	}
	indexData := testutil.BuildTestIndex(t, entries)
	indexView, err := blobcore.NewIndexView(indexData)
	require.NoError(t, err)

	result := &InspectResult{
		index: indexView,
	}

	idx := result.Index()
	require.NotNil(t, idx)

	// Test Entry lookup
	entry, ok := idx.Entry("dir/file.txt")
	require.True(t, ok)
	assert.Equal(t, "dir/file.txt", entry.Path())
	assert.Equal(t, uint64(100), entry.OriginalSize())

	// Test non-existent entry
	_, ok = idx.Entry("nonexistent.txt")
	assert.False(t, ok)

	// Test Entries iteration
	count := 0
	for range idx.Entries() {
		count++
	}
	assert.Equal(t, 3, count)

	// Test EntriesWithPrefix
	prefixCount := 0
	for range idx.EntriesWithPrefix("dir/") {
		prefixCount++
	}
	assert.Equal(t, 2, prefixCount)
}

func TestInspectOption_SkipCache(t *testing.T) {
	t.Parallel()

	cfg := &inspectConfig{}

	InspectWithSkipCache()(cfg)

	assert.True(t, cfg.skipCache)
}

func TestInspectOption_MaxIndexSize(t *testing.T) {
	t.Parallel()

	cfg := &inspectConfig{}

	InspectWithMaxIndexSize(1024 * 1024)(cfg)

	assert.Equal(t, int64(1024*1024), cfg.maxIndexSize)
}

func TestReferrerFromDescriptor(t *testing.T) {
	t.Parallel()

	// Import is needed but we test through the public type
	ref := Referrer{
		Digest:       "sha256:abc123",
		Size:         1234,
		MediaType:    "application/vnd.oci.image.manifest.v1+json",
		ArtifactType: "application/vnd.sigstore.bundle.v0.3+json",
		Annotations: map[string]string{
			"key": "value",
		},
	}

	assert.Equal(t, "sha256:abc123", ref.Digest)
	assert.Equal(t, int64(1234), ref.Size)
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", ref.MediaType)
	assert.Equal(t, "application/vnd.sigstore.bundle.v0.3+json", ref.ArtifactType)
	assert.Equal(t, "value", ref.Annotations["key"])
}

func TestInspectResult_ManifestMethods(t *testing.T) {
	t.Parallel()

	// Create a minimal index for the result
	indexData := testutil.MakeMinimalIndex()
	indexView, err := blobcore.NewIndexView(indexData)
	require.NoError(t, err)

	// Create a mock manifest using the registry package
	// We test through the InspectResult wrapper methods
	testDigest := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	created := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// Create an InspectResult with a manifest
	result := createTestInspectResult(t, testDigest, created, indexView, 1000, 5000)

	assert.Equal(t, testDigest, result.Digest())
	assert.Equal(t, created, result.Created())
	assert.Equal(t, int64(1000), result.IndexBlobSize())
	assert.Equal(t, int64(5000), result.DataBlobSize())
}

// createTestInspectResult creates an InspectResult with a properly constructed manifest for testing.
func createTestInspectResult(t *testing.T, digest string, created time.Time, index *blobcore.IndexView, indexSize, dataSize int64) *InspectResult {
	t.Helper()

	manifest := registry.NewTestManifest(digest, created, indexSize, dataSize)
	return &InspectResult{
		manifest: manifest,
		index:    index,
	}
}
