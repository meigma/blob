package blobtype

// ProgressEvent represents a progress update during push, pull, or extraction operations.
type ProgressEvent struct {
	// Stage identifies the current phase of the operation.
	Stage ProgressStage

	// Path is the file currently being processed, if applicable.
	Path string

	// BytesDone is the number of bytes completed in the current operation.
	BytesDone uint64

	// BytesTotal is the total bytes for the current operation.
	// Zero indicates the total is unknown.
	BytesTotal uint64

	// FilesDone is the number of files completed.
	FilesDone int

	// FilesTotal is the total number of files.
	// Zero indicates the total is unknown (e.g., during enumeration).
	FilesTotal int
}

// ProgressStage identifies the current phase of an operation.
type ProgressStage uint8

// Progress stages for push, pull, and extraction operations.
const (
	// StageEnumerating indicates the operation is walking the directory tree.
	StageEnumerating ProgressStage = iota

	// StageCompressing indicates files are being compressed and written.
	StageCompressing

	// StagePushingIndex indicates the index blob is being uploaded.
	StagePushingIndex

	// StagePushingData indicates the data blob is being uploaded.
	StagePushingData

	// StageFetchingManifest indicates the manifest is being fetched.
	StageFetchingManifest

	// StageFetchingIndex indicates the index blob is being fetched.
	StageFetchingIndex

	// StageExtracting indicates files are being extracted.
	StageExtracting
)

// String returns the string representation of the stage.
func (s ProgressStage) String() string {
	switch s {
	case StageEnumerating:
		return "enumerating"
	case StageCompressing:
		return "compressing"
	case StagePushingIndex:
		return "pushing index"
	case StagePushingData:
		return "pushing data"
	case StageFetchingManifest:
		return "fetching manifest"
	case StageFetchingIndex:
		return "fetching index"
	case StageExtracting:
		return "extracting"
	default:
		return "unknown"
	}
}

// ProgressFunc receives progress updates during operations.
// Implementations must be safe for concurrent calls.
type ProgressFunc func(ProgressEvent)
