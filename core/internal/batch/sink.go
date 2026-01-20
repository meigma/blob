package batch

import (
	"io"

	"github.com/meigma/blob/core/internal/blobtype"
)

// Entry is an alias for blobtype.Entry.
type Entry = blobtype.Entry

// Sink receives decompressed and verified file content during batch processing.
//
// Implementations determine where content is written (cache, filesystem, etc.)
// and can filter which entries to process.
type Sink interface {
	// ShouldProcess returns false if this entry should be skipped.
	// This allows implementations to skip already-cached entries or existing files.
	ShouldProcess(entry *Entry) bool

	// Writer returns a writer for the entry's content.
	// The returned Committer must have Commit() called after successful
	// write and verification, or Discard() called on any error.
	//
	// The caller will:
	// 1. Write decompressed content to the Committer
	// 2. Verify the SHA256 hash matches entry.Hash
	// 3. Call Commit() if verification succeeds, Discard() otherwise
	Writer(entry *Entry) (Committer, error)
}

// BufferedSink allows sinks to handle decoded content without copying.
//
// Implementations should not mutate the content slice.
type BufferedSink interface {
	PutBuffered(entry *Entry, content []byte) error
}

// Committer is a writer that can be committed or discarded.
//
// Implementations should buffer or stage writes until Commit is called.
// For example, a file-based implementation might write to a temp file
// and rename it on Commit, or delete it on Discard.
type Committer interface {
	io.Writer

	// Commit finalizes the write, making content available.
	// Must be called after successful hash verification.
	Commit() error

	// Discard aborts the write and cleans up any temporary resources.
	// Must be called if verification fails or an error occurs.
	Discard() error
}
