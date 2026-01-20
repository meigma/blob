package blob

import (
	"iter"

	"github.com/meigma/blob/core/internal/blobtype"
	"github.com/meigma/blob/core/internal/index"
)

// IndexView provides read-only access to archive file metadata.
//
// It exposes index iteration and lookup without requiring
// the data blob to be available. This is useful for inspecting
// archive contents before deciding to download file data.
//
// IndexView methods mirror those on [Blob] for consistency.
type IndexView struct {
	idx       *index.Index
	indexData []byte
}

// NewIndexView creates an IndexView from raw FlatBuffers-encoded index data.
//
// The provided data is retained by the IndexView; callers must not modify it
// after calling NewIndexView.
func NewIndexView(indexData []byte) (*IndexView, error) {
	idx, err := index.Load(indexData)
	if err != nil {
		return nil, err
	}
	return &IndexView{
		idx:       idx,
		indexData: indexData,
	}, nil
}

// Len returns the number of files in the archive.
func (v *IndexView) Len() int {
	return v.idx.Len()
}

// Version returns the index format version.
func (v *IndexView) Version() uint32 {
	return v.idx.Version()
}

// DataHash returns the SHA256 hash of the data blob.
// The returned slice aliases the index buffer and must be treated as immutable.
// ok is false when the index did not record data metadata.
func (v *IndexView) DataHash() ([]byte, bool) {
	return v.idx.DataHash()
}

// DataSize returns the size of the data blob in bytes.
// ok is false when the index did not record data metadata.
func (v *IndexView) DataSize() (uint64, bool) {
	return v.idx.DataSize()
}

// Entry returns a read-only view of the entry for the given path.
//
// The returned view is only valid while the IndexView remains alive.
func (v *IndexView) Entry(path string) (blobtype.EntryView, bool) {
	return v.idx.LookupView(path)
}

// Entries returns an iterator over all file entries.
//
// The returned views are only valid while the IndexView remains alive.
func (v *IndexView) Entries() iter.Seq[blobtype.EntryView] {
	return v.idx.EntriesView()
}

// EntriesWithPrefix returns an iterator over entries with the given prefix.
//
// The returned views are only valid while the IndexView remains alive.
func (v *IndexView) EntriesWithPrefix(prefix string) iter.Seq[blobtype.EntryView] {
	return v.idx.EntriesWithPrefixView(prefix)
}

// IndexData returns the raw FlatBuffers-encoded index.
// This is useful for caching or transmitting the index.
func (v *IndexView) IndexData() []byte {
	return v.indexData
}
