package testutil

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/fb"
)

// TestEntry holds data for building test index entries.
type TestEntry struct {
	Path         string
	DataOffset   uint64
	DataSize     uint64
	OriginalSize uint64
	Hash         []byte
	Mode         fs.FileMode
	UID          uint32
	GID          uint32
	ModTime      time.Time
	Compression  blobtype.Compression
}

// BuildTestIndex creates a FlatBuffers-encoded index from test entries.
// Entries are automatically sorted by path (required for binary search).
func BuildTestIndex(tb testing.TB, entries []TestEntry) []byte {
	tb.Helper()

	// Sort entries by path (required for EntriesByKey to work)
	slices.SortFunc(entries, func(a, b TestEntry) int {
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
		fb.EntryAddCompression(builder, fb.Compression(e.Compression)) //nolint:gosec // Compression is bounded 0-1
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
