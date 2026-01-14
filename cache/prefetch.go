package cache

import "github.com/meigma/blob/internal/fileops"

// rangeGroup represents a contiguous range of entries in the data blob.
type rangeGroup struct {
	start   uint64
	end     uint64
	entries []fileops.Entry
}

// groupAdjacentEntries groups entries that are adjacent in the data blob.
func groupAdjacentEntries(entries []fileops.Entry) []rangeGroup {
	groups := make([]rangeGroup, 0, len(entries))
	current := rangeGroup{
		start:   entries[0].DataOffset,
		end:     entries[0].DataOffset + entries[0].DataSize,
		entries: []fileops.Entry{entries[0]},
	}

	for i := 1; i < len(entries); i++ {
		entry := entries[i]
		entryEnd := entry.DataOffset + entry.DataSize

		if entry.DataOffset == current.end {
			current.end = entryEnd
			current.entries = append(current.entries, entry)
		} else {
			groups = append(groups, current)
			current = rangeGroup{
				start:   entry.DataOffset,
				end:     entryEnd,
				entries: []fileops.Entry{entry},
			}
		}
	}
	return append(groups, current)
}
