package registry

import "errors"

// Sentinel errors for client operations.
var (
	// ErrNotFound is returned when a blob archive does not exist at the reference.
	ErrNotFound = errors.New("client: not found")

	// ErrInvalidReference is returned when a reference string is malformed.
	ErrInvalidReference = errors.New("client: invalid reference")

	// ErrInvalidManifest is returned when a manifest is not a valid blob archive manifest.
	ErrInvalidManifest = errors.New("client: invalid blob manifest")

	// ErrMissingIndex is returned when the manifest does not contain an index blob.
	ErrMissingIndex = errors.New("client: missing index blob")

	// ErrMissingData is returned when the manifest does not contain a data blob.
	ErrMissingData = errors.New("client: missing data blob")

	// ErrDigestMismatch is returned when content does not match its expected digest.
	ErrDigestMismatch = errors.New("client: digest mismatch")

	// ErrPolicyViolation is returned when a policy rejects a manifest.
	ErrPolicyViolation = errors.New("client: policy violation")

	// ErrReferrersUnsupported is returned when referrers are not supported by the OCI client.
	ErrReferrersUnsupported = errors.New("client: referrers unsupported")
)
