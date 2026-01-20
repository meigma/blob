package blob

import (
	blobcore "github.com/meigma/blob/core"
	"github.com/meigma/blob/registry"
)

// Errors re-exported from core.
var (
	// ErrHashMismatch is returned when file content hash doesn't match the expected hash.
	ErrHashMismatch = blobcore.ErrHashMismatch

	// ErrDecompression is returned when decompression fails.
	ErrDecompression = blobcore.ErrDecompression

	// ErrSizeOverflow is returned when a size value overflows.
	ErrSizeOverflow = blobcore.ErrSizeOverflow

	// ErrSymlink is returned when a symlink is encountered during archive creation.
	ErrSymlink = blobcore.ErrSymlink

	// ErrTooManyFiles is returned when the archive contains more files than allowed.
	ErrTooManyFiles = blobcore.ErrTooManyFiles
)

// Errors re-exported from registry.
var (
	// ErrNotFound is returned when a blob archive does not exist at the reference.
	ErrNotFound = registry.ErrNotFound

	// ErrInvalidReference is returned when a reference string is malformed.
	ErrInvalidReference = registry.ErrInvalidReference

	// ErrInvalidManifest is returned when a manifest is not a valid blob archive manifest.
	ErrInvalidManifest = registry.ErrInvalidManifest

	// ErrMissingIndex is returned when the manifest does not contain an index blob.
	ErrMissingIndex = registry.ErrMissingIndex

	// ErrMissingData is returned when the manifest does not contain a data blob.
	ErrMissingData = registry.ErrMissingData

	// ErrDigestMismatch is returned when content does not match its expected digest.
	ErrDigestMismatch = registry.ErrDigestMismatch

	// ErrPolicyViolation is returned when a policy rejects a manifest.
	ErrPolicyViolation = registry.ErrPolicyViolation

	// ErrReferrersUnsupported is returned when referrers are not supported by the OCI client.
	ErrReferrersUnsupported = registry.ErrReferrersUnsupported
)
