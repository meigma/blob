package registry

import (
	blob "github.com/meigma/blob/core"
	"github.com/meigma/blob/core/cache"
)

// PullOption configures a Pull operation.
type PullOption func(*pullConfig)

type pullConfig struct {
	skipCache bool
	blobOpts  []blob.Option
	// maxIndexSize limits how many bytes are read for the index blob.
	// A value <= 0 disables the limit.
	maxIndexSize int64
	progress     blob.ProgressFunc
	blockCache   cache.BlockCache
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

// WithPullProgress sets a callback to receive progress updates during pull.
// The callback receives events for manifest and index fetching.
// The callback may be invoked concurrently and must be safe for concurrent use.
func WithPullProgress(fn blob.ProgressFunc) PullOption {
	return func(cfg *pullConfig) {
		cfg.progress = fn
	}
}

// WithBlockCache sets a block cache to wrap the HTTP data source.
// This caches HTTP range request blocks for improved performance on
// random access patterns.
func WithBlockCache(bc cache.BlockCache) PullOption {
	return func(cfg *pullConfig) {
		cfg.blockCache = bc
	}
}
