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

// Index provides access to archive entries.
//
// Index is backed by FlatBuffers and provides O(log n) lookups by path.
// Entries are sorted by path, enabling efficient prefix scans for directory operations.
type Index struct {
	data []byte
	root *fb.Index
}

// LoadIndex parses a FlatBuffers-encoded index blob.
//
// The provided data is retained by the Index; callers must not modify it
// after calling LoadIndex.
func LoadIndex(data []byte) (*Index, error) {
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

// Lookup returns the entry for the given path.
// Returns false if the path does not exist in the index.
//
// Lookup uses binary search and completes in O(log n) time.
func (idx *Index) Lookup(path string) (Entry, bool) {
	var fbEntry fb.Entry
	if !idx.root.EntriesByKey(&fbEntry, path) {
		return Entry{}, false
	}
	return entryFromFlatBuffers(&fbEntry), true
}

// Len returns the number of entries in the index.
func (idx *Index) Len() int {
	return idx.root.EntriesLength()
}

// Entries returns an iterator over all entries in path-sorted order.
func (idx *Index) Entries() iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		var fbEntry fb.Entry
		for i := range idx.root.EntriesLength() {
			if !idx.root.Entries(&fbEntry, i) {
				return
			}
			if !yield(entryFromFlatBuffers(&fbEntry)) {
				return
			}
		}
	}
}

// EntriesWithPrefix returns an iterator over entries whose paths begin with prefix.
//
// This is useful for directory operations—all files under a directory share
// a common prefix and are stored adjacently in both the index and data blob.
func (idx *Index) EntriesWithPrefix(prefix string) iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		n := idx.root.EntriesLength()
		if n == 0 {
			return
		}
		prefixBytes := []byte(prefix)

		// Binary search to find the first entry with path >= prefix
		start := sort.Search(n, func(i int) bool {
			var fbEntry fb.Entry
			if !idx.root.Entries(&fbEntry, i) {
				return false
			}
			return bytes.Compare(fbEntry.Path(), prefixBytes) >= 0
		})

		// Iterate while prefix matches
		var fbEntry fb.Entry
		for i := start; i < n; i++ {
			if !idx.root.Entries(&fbEntry, i) {
				return
			}
			pathBytes := fbEntry.Path()
			if !bytes.HasPrefix(pathBytes, prefixBytes) {
				return
			}
			if !yield(entryFromFlatBuffers(&fbEntry)) {
				return
			}
		}
	}
}

// entryFromFlatBuffers converts a FlatBuffers Entry to a blob.Entry.
func entryFromFlatBuffers(entry *fb.Entry) Entry {
	// Copy hash bytes since FlatBuffers data is shared
	hashLen := entry.HashLength()
	hash := make([]byte, hashLen)
	for i := range hashLen {
		hash[i] = entry.Hash(i)
	}

	// Convert compression, defaulting to None for invalid values
	comp := CompressionNone
	if c := int8(entry.Compression()); c >= 0 && c <= int8(CompressionZstd) {
		comp = Compression(c) //nolint:gosec // bounds checked above
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
		Compression:  comp,
	}
}
