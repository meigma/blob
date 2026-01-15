package index

import (
	"bytes"
	"errors"
	"iter"
	"sort"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/fb"
)

// Index provides access to archive entries.
//
// Index is backed by FlatBuffers and provides O(log n) lookups by path.
// Entries are sorted by path, enabling efficient prefix scans for directory operations.
//
// Accessors return read-only EntryView values that alias index data.
type Index struct {
	data []byte
	root *fb.Index
}

// Load parses a FlatBuffers-encoded index blob.
//
// The provided data is retained by the index; callers must not modify it
// after calling Load.
func Load(data []byte) (*Index, error) {
	if len(data) == 0 {
		return nil, errors.New("blob: empty index data")
	}

	root := fb.GetRootAsIndex(data, 0)
	if root == nil {
		return nil, errors.New("blob: failed to parse index")
	}

	return &Index{
		data: data,
		root: root,
	}, nil
}

// Version returns the protocol version of the index.
func (idx *Index) Version() uint32 {
	return idx.root.Version()
}

// LookupView returns a read-only view of the entry for the given path.
//
// The returned view is only valid while the index remains alive.
func (idx *Index) LookupView(path string) (blobtype.EntryView, bool) {
	var fbEntry fb.Entry
	if !idx.root.EntriesByKey(&fbEntry, path) {
		return blobtype.EntryView{}, false
	}
	return blobtype.EntryViewFromFlatBuffers(fbEntry), true
}

// Len returns the number of entries in the index.
func (idx *Index) Len() int {
	return idx.root.EntriesLength()
}

// EntriesView returns an iterator over all entries as read-only views.
//
// The returned views are only valid while the index remains alive.
func (idx *Index) EntriesView() iter.Seq[blobtype.EntryView] {
	return func(yield func(blobtype.EntryView) bool) {
		var fbEntry fb.Entry
		for i := range idx.root.EntriesLength() {
			if !idx.root.Entries(&fbEntry, i) {
				return
			}
			if !yield(blobtype.EntryViewFromFlatBuffers(fbEntry)) {
				return
			}
		}
	}
}

// EntriesWithPrefixView returns an iterator over entries with the given prefix
// as read-only views.
//
// The returned views are only valid while the index remains alive.
func (idx *Index) EntriesWithPrefixView(prefix string) iter.Seq[blobtype.EntryView] {
	return func(yield func(blobtype.EntryView) bool) {
		n := idx.root.EntriesLength()
		if n == 0 {
			return
		}
		prefixBytes := []byte(prefix)

		start := sort.Search(n, func(i int) bool {
			var fbEntry fb.Entry
			if !idx.root.Entries(&fbEntry, i) {
				return false
			}
			return bytes.Compare(fbEntry.Path(), prefixBytes) >= 0
		})

		var fbEntry fb.Entry
		for i := start; i < n; i++ {
			if !idx.root.Entries(&fbEntry, i) {
				return
			}
			pathBytes := fbEntry.Path()
			if !bytes.HasPrefix(pathBytes, prefixBytes) {
				return
			}
			if !yield(blobtype.EntryViewFromFlatBuffers(fbEntry)) {
				return
			}
		}
	}
}
