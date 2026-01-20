package blob

import blobcore "github.com/meigma/blob/core"

// PullOption configures a Pull operation.
type PullOption func(*pullConfig)

type pullConfig struct {
	skipCache    bool
	maxIndexSize int64
	blobOpts     []blobcore.Option
}

// PullWithSkipCache bypasses the ref and manifest caches.
//
// This forces a fresh fetch from the registry even if cached data exists.
func PullWithSkipCache() PullOption {
	return func(cfg *pullConfig) {
		cfg.skipCache = true
	}
}

// PullWithMaxIndexSize sets the maximum number of bytes allowed for the index blob.
//
// Use a value <= 0 to disable the limit.
func PullWithMaxIndexSize(maxBytes int64) PullOption {
	return func(cfg *pullConfig) {
		cfg.maxIndexSize = maxBytes
	}
}

// --- Decoder options (passed to core.Blob) ---

// PullWithMaxFileSize limits the maximum per-file size (compressed and uncompressed).
// Set limit to 0 to disable the limit.
func PullWithMaxFileSize(limit uint64) PullOption {
	return func(cfg *pullConfig) {
		cfg.blobOpts = append(cfg.blobOpts, blobcore.WithMaxFileSize(limit))
	}
}

// PullWithDecoderConcurrency sets the zstd decoder concurrency (default: 1).
// Values < 0 are treated as 0 (use GOMAXPROCS).
func PullWithDecoderConcurrency(n int) PullOption {
	return func(cfg *pullConfig) {
		cfg.blobOpts = append(cfg.blobOpts, blobcore.WithDecoderConcurrency(n))
	}
}

// PullWithDecoderLowmem sets whether the zstd decoder should use low-memory mode (default: false).
func PullWithDecoderLowmem(enabled bool) PullOption {
	return func(cfg *pullConfig) {
		cfg.blobOpts = append(cfg.blobOpts, blobcore.WithDecoderLowmem(enabled))
	}
}

// PullWithVerifyOnClose controls whether Close drains the file to verify the hash.
//
// When false, Close returns without reading the remaining data. Integrity is
// only guaranteed when callers read to EOF.
func PullWithVerifyOnClose(enabled bool) PullOption {
	return func(cfg *pullConfig) {
		cfg.blobOpts = append(cfg.blobOpts, blobcore.WithVerifyOnClose(enabled))
	}
}
