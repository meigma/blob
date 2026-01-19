package blob

import blobcore "github.com/meigma/blob/core"

// --- Re-exports from core ---

// Compression identifies the compression algorithm used for a file.
type Compression = blobcore.Compression

// Entry represents a file in the archive.
type Entry = blobcore.Entry

// EntryView provides a read-only view of an index entry.
type EntryView = blobcore.EntryView

// ChangeDetection controls how strictly file changes are detected during creation.
type ChangeDetection = blobcore.ChangeDetection

// SkipCompressionFunc returns true when a file should be stored uncompressed.
type SkipCompressionFunc = blobcore.SkipCompressionFunc

// CopyOption configures CopyTo and CopyDir operations.
type CopyOption = blobcore.CopyOption

// ByteSource provides random access to the data blob.
type ByteSource = blobcore.ByteSource

// Compression constants.
const (
	CompressionNone = blobcore.CompressionNone
	CompressionZstd = blobcore.CompressionZstd
)

// ChangeDetection constants.
const (
	ChangeDetectionNone   = blobcore.ChangeDetectionNone
	ChangeDetectionStrict = blobcore.ChangeDetectionStrict
)

// Copy options re-exported from core.
var (
	CopyWithOverwrite       = blobcore.CopyWithOverwrite
	CopyWithPreserveMode    = blobcore.CopyWithPreserveMode
	CopyWithPreserveTimes   = blobcore.CopyWithPreserveTimes
	CopyWithCleanDest       = blobcore.CopyWithCleanDest
	CopyWithWorkers         = blobcore.CopyWithWorkers
	CopyWithReadConcurrency = blobcore.CopyWithReadConcurrency
)

// DefaultSkipCompression returns a SkipCompressionFunc that skips small files
// and known already-compressed extensions.
var DefaultSkipCompression = blobcore.DefaultSkipCompression

// DefaultMaxFiles is the default limit used when no MaxFiles option is set.
const DefaultMaxFiles = blobcore.DefaultMaxFiles

// Default file names for blob archives.
const (
	DefaultIndexName = blobcore.DefaultIndexName
	DefaultDataName  = blobcore.DefaultDataName
)
