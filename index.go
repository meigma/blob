package blob

import (
	"bytes"
	"errors"
	"io/fs"
	"iter"
	"sort"
	"time"

	"github.com/meigma/blob/internal/fb"
)

// index provides access to archive entries.
//
// index is backed by FlatBuffers and provides O(log n) lookups by path.
// Entries are sorted by path, enabling efficient prefix scans for directory operations.
//
// Accessors return read-only EntryView values that alias index data.
type index struct {
	data []byte
	root *fb.Index
}

// loadIndex parses a FlatBuffers-encoded index blob.
//
// The provided data is retained by the index; callers must not modify it
// after calling loadIndex.
func loadIndex(data []byte) (*index, error) {
	if len(data) == 0 {
		return nil, errors.New("blob: empty index data")
	}

	root := fb.GetRootAsIndex(data, 0)
	if root == nil {
		return nil, errors.New("blob: failed to parse index")
	}

	return &index{
		data: data,
		root: root,
	}, nil
}

// version returns the protocol version of the index.
func (idx *index) version() uint32 {
	return idx.root.Version()
}

// lookupView returns a read-only view of the entry for the given path.
//
// The returned view is only valid while the index remains alive.
func (idx *index) lookupView(path string) (EntryView, bool) {
	var fbEntry fb.Entry
	if !idx.root.EntriesByKey(&fbEntry, path) {
		return EntryView{}, false
	}
	return entryViewFromFlatBuffers(fbEntry), true
}

// len returns the number of entries in the index.
func (idx *index) len() int {
	return idx.root.EntriesLength()
}

// entriesView returns an iterator over all entries as read-only views.
//
// The returned views are only valid while the index remains alive.
func (idx *index) entriesView() iter.Seq[EntryView] {
	return func(yield func(EntryView) bool) {
		var fbEntry fb.Entry
		for i := range idx.root.EntriesLength() {
			if !idx.root.Entries(&fbEntry, i) {
				return
			}
			if !yield(entryViewFromFlatBuffers(fbEntry)) {
				return
			}
		}
	}
}

// entriesWithPrefixView returns an iterator over entries with the given prefix
// as read-only views.
//
// The returned views are only valid while the index remains alive.
func (idx *index) entriesWithPrefixView(prefix string) iter.Seq[EntryView] {
	return func(yield func(EntryView) bool) {
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
			if !yield(entryViewFromFlatBuffers(fbEntry)) {
				return
			}
		}
	}
}

// entryFromFlatBuffers converts a FlatBuffers Entry to a blob.Entry.
func entryFromFlatBuffers(entry *fb.Entry) Entry {
	// Copy hash bytes since FlatBuffers data is shared.
	hashLen := entry.HashLength()
	hash := make([]byte, hashLen)
	for i := range hashLen {
		hash[i] = entry.Hash(i)
	}

	return Entry{
		Path:         string(entry.Path()),
		DataOffset:   entry.DataOffset(),
		DataSize:     entry.DataSize(),
		OriginalSize: entry.OriginalSize(),
		Hash:         hash,
		Mode:         fs.FileMode(entry.Mode()),
		UID:          entry.Uid(),
		GID:          entry.Gid(),
		ModTime:      time.Unix(0, entry.MtimeNs()),
		Compression:  compressionFromFB(entry.Compression()),
	}
}

func compressionFromFB(c fb.Compression) Compression {
	if v := int8(c); v >= 0 && v <= int8(CompressionZstd) {
		return Compression(v) //nolint:gosec // bounds checked above
	}
	return CompressionNone
}
