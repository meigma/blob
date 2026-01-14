package blob

import (
	"io/fs"
	"time"

	"github.com/meigma/blob/internal/fb"
)

// EntryView provides a read-only view of an index entry.
//
// The byte slices returned by PathBytes and HashBytes alias the index buffer
// and must be treated as immutable. The view is only valid while the Index
// that produced it remains alive.
type EntryView struct {
	entry fb.Entry
}

// PathBytes returns the path bytes from the index buffer.
func (ev EntryView) PathBytes() []byte {
	return ev.entry.Path()
}

// HashBytes returns the SHA256 hash bytes from the index buffer.
func (ev EntryView) HashBytes() []byte {
	return ev.entry.HashBytes()
}

// Path returns the path as a string.
func (ev EntryView) Path() string {
	return string(ev.entry.Path())
}

// DataOffset returns the data blob offset for this entry.
func (ev EntryView) DataOffset() uint64 {
	return ev.entry.DataOffset()
}

// DataSize returns the stored (possibly compressed) size.
func (ev EntryView) DataSize() uint64 {
	return ev.entry.DataSize()
}

// OriginalSize returns the uncompressed size.
func (ev EntryView) OriginalSize() uint64 {
	return ev.entry.OriginalSize()
}

// Mode returns the file mode bits.
func (ev EntryView) Mode() fs.FileMode {
	return fs.FileMode(ev.entry.Mode())
}

// UID returns the file owner's user ID.
func (ev EntryView) UID() uint32 {
	return ev.entry.Uid()
}

// GID returns the file owner's group ID.
func (ev EntryView) GID() uint32 {
	return ev.entry.Gid()
}

// ModTime returns the modification time.
func (ev EntryView) ModTime() time.Time {
	return time.Unix(0, ev.entry.MtimeNs())
}

// Compression returns the compression algorithm used.
func (ev EntryView) Compression() Compression {
	return compressionFromFB(ev.entry.Compression())
}

// Entry returns a fully copied Entry.
func (ev EntryView) Entry() Entry {
	return entryFromFlatBuffers(&ev.entry)
}

func entryViewFromFlatBuffers(entry fb.Entry) EntryView {
	return EntryView{entry: entry}
}

func entryFromViewWithPath(ev EntryView, path string) Entry {
	return Entry{
		Path:         path,
		DataOffset:   ev.DataOffset(),
		DataSize:     ev.DataSize(),
		OriginalSize: ev.OriginalSize(),
		Hash:         ev.HashBytes(),
		Mode:         ev.Mode(),
		UID:          ev.UID(),
		GID:          ev.GID(),
		ModTime:      ev.ModTime(),
		Compression:  ev.Compression(),
	}
}

func entryFromView(ev EntryView) Entry {
	return entryFromViewWithPath(ev, string(ev.PathBytes()))
}
