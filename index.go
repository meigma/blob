package blob

import "iter"

// Index provides access to archive entries.
//
// Index is backed by FlatBuffers and provides O(log n) lookups by path.
// Entries are sorted by path, enabling efficient prefix scans for directory operations.
type Index struct {
	// TODO: FlatBuffers data and parsed root
}

// LoadIndex parses a FlatBuffers-encoded index blob.
//
// The provided data is retained by the Index; callers must not modify it
// after calling LoadIndex.
func LoadIndex(data []byte) (*Index, error) {
	panic("not implemented")
}

// Lookup returns the entry for the given path.
// Returns false if the path does not exist in the index.
//
// Lookup uses binary search and completes in O(log n) time.
func (idx *Index) Lookup(path string) (Entry, bool) {
	panic("not implemented")
}

// Len returns the number of entries in the index.
func (idx *Index) Len() int {
	panic("not implemented")
}

// Entries returns an iterator over all entries in path-sorted order.
func (idx *Index) Entries() iter.Seq[Entry] {
	panic("not implemented")
}

// EntriesWithPrefix returns an iterator over entries whose paths begin with prefix.
//
// This is useful for directory operations—all files under a directory share
// a common prefix and are stored adjacently in both the index and data blob.
func (idx *Index) EntriesWithPrefix(prefix string) iter.Seq[Entry] {
	panic("not implemented")
}
