package client

import "github.com/meigma/blob"

// PullOption configures a Pull operation.
type PullOption func(*pullConfig)

type pullConfig struct {
	blobOpts []blob.Option
}

// WithBlobOptions passes options to the created Blob.
//
// These options configure the Blob's behavior, such as decoder settings
// and verification options.
func WithBlobOptions(opts ...blob.Option) PullOption {
	panic("not implemented")
}
