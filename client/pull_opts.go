package client

import "github.com/meigma/blob/core"

// PullOption configures a Pull operation.
type PullOption func(*pullConfig)

type pullConfig struct {
	skipCache bool
	blobOpts  []blob.Option
	// maxIndexSize limits how many bytes are read for the index blob.
	// A value <= 0 disables the limit.
	maxIndexSize int64
}

const defaultMaxIndexSize = 8 << 20 // 8 MiB

// WithBlobOptions passes options to the created Blob.
//
// These options configure the Blob's behavior, such as decoder settings
// and verification options.
func WithBlobOptions(opts ...blob.Option) PullOption {
	return func(cfg *pullConfig) {
		cfg.blobOpts = append(cfg.blobOpts, opts...)
	}
}

// WithMaxIndexSize sets the maximum number of bytes allowed for the index blob.
//
// Use a value <= 0 to disable the limit.
func WithMaxIndexSize(maxBytes int64) PullOption {
	return func(cfg *pullConfig) {
		cfg.maxIndexSize = maxBytes
	}
}

// WithPullSkipCache bypasses the ref and manifest caches.
//
// This forces a fresh fetch from the registry even if cached data exists.
func WithPullSkipCache() PullOption {
	return func(cfg *pullConfig) {
		cfg.skipCache = true
	}
}
