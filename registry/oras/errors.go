package oras

import "errors"

// Sentinel errors for OCI operations.
var (
	// ErrNotFound is returned when a blob or manifest does not exist.
	ErrNotFound = errors.New("oci: not found")

	// ErrUnauthorized is returned when authentication fails.
	ErrUnauthorized = errors.New("oci: unauthorized")

	// ErrForbidden is returned when access is denied.
	ErrForbidden = errors.New("oci: forbidden")

	// ErrInvalidReference is returned when a reference string is malformed.
	ErrInvalidReference = errors.New("oci: invalid reference")

	// ErrInvalidDescriptor is returned when a descriptor is nil or has invalid fields.
	ErrInvalidDescriptor = errors.New("oci: invalid descriptor")

	// ErrManifestInvalid is returned when a manifest cannot be parsed.
	ErrManifestInvalid = errors.New("oci: invalid manifest")

	// ErrDigestMismatch is returned when content does not match its digest.
	ErrDigestMismatch = errors.New("oci: digest mismatch")

	// ErrSizeMismatch is returned when content size does not match expected size.
	ErrSizeMismatch = errors.New("oci: size mismatch")

	// ErrReferrersUnsupported is returned when the registry does not support referrers.
	ErrReferrersUnsupported = errors.New("oci: referrers unsupported")
)
