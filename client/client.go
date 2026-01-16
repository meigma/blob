// Package client provides a high-level client for pushing and pulling
// blob archives to/from OCI registries.
//
// The client uses the oci subpackage for low-level OCI operations and
// adds blob-archive-specific functionality like manifest caching and
// lazy blob access via HTTP range requests.
package client

import (
	"github.com/meigma/blob/client/oci"
)

// Client provides high-level operations for blob archives in OCI registries.
type Client struct {
	oci           *oci.Client
	refCache      RefCache
	manifestCache ManifestCache
}

// New creates a new blob archive client with the given options.
func New(opts ...Option) *Client {
	panic("not implemented")
}
