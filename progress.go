package blob

import blobcore "github.com/meigma/blob/core"

// Re-export progress types from core package.
type (
	// ProgressEvent represents a progress update during push, pull, or extraction operations.
	ProgressEvent = blobcore.ProgressEvent

	// ProgressStage identifies the current phase of an operation.
	ProgressStage = blobcore.ProgressStage

	// ProgressFunc receives progress updates during operations.
	// Implementations must be safe for concurrent calls.
	ProgressFunc = blobcore.ProgressFunc
)

// Re-export progress stage constants.
const (
	// StageEnumerating indicates the operation is walking the directory tree.
	StageEnumerating = blobcore.StageEnumerating

	// StageCompressing indicates files are being compressed and written.
	StageCompressing = blobcore.StageCompressing

	// StagePushingIndex indicates the index blob is being uploaded.
	StagePushingIndex = blobcore.StagePushingIndex

	// StagePushingData indicates the data blob is being uploaded.
	StagePushingData = blobcore.StagePushingData

	// StageFetchingManifest indicates the manifest is being fetched.
	StageFetchingManifest = blobcore.StageFetchingManifest

	// StageFetchingIndex indicates the index blob is being fetched.
	StageFetchingIndex = blobcore.StageFetchingIndex

	// StageExtracting indicates files are being extracted.
	StageExtracting = blobcore.StageExtracting
)
