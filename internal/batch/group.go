package batch

// rangeGroup represents a contiguous range of entries in the data blob.
// All entries in a group can be fetched with a single range request.
type rangeGroup struct {
	start   uint64   // Start byte offset in data blob
	end     uint64   // End byte offset (exclusive) in data blob
	entries []*Entry // Entries within this range
}

// groupAdjacentEntries groups entries that are adjacent in the data blob.
//
// Entries must be sorted by DataOffset before calling this function.
// Adjacent entries (where one ends exactly where the next begins) are
// combined into a single group to enable efficient batched reads.
//
// The entries slice must be non-empty.
func groupAdjacentEntries(entries []*Entry) []rangeGroup {
	groups := make([]rangeGroup, 0, len(entries))
	current := rangeGroup{
		start:   entries[0].DataOffset,
		end:     entries[0].DataOffset + entries[0].DataSize,
		entries: []*Entry{entries[0]},
	}

	for i := 1; i < len(entries); i++ {
		entry := entries[i]
		entryEnd := entry.DataOffset + entry.DataSize

		if entry.DataOffset == current.end {
			// Entry is adjacent - extend current group
			current.end = entryEnd
			current.entries = append(current.entries, entry)
		} else {
			// Gap between entries - start new group
			groups = append(groups, current)
			current = rangeGroup{
				start:   entry.DataOffset,
				end:     entryEnd,
				entries: []*Entry{entry},
			}
		}
	}
	return append(groups, current)
}
