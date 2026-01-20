package blob

import "github.com/meigma/blob/core/cache"

// Option configures a Blob.
type Option func(*Blob)

// WithMaxFileSize limits the maximum per-file size (compressed and uncompressed).
// Set limit to 0 to disable the limit.
func WithMaxFileSize(limit uint64) Option {
	return func(b *Blob) {
		b.maxFileSize = limit
	}
}

// WithMaxDecoderMemory limits the maximum memory used by the zstd decoder.
// Set limit to 0 to disable the limit.
func WithMaxDecoderMemory(limit uint64) Option {
	return func(b *Blob) {
		b.maxDecoderMemory = limit
	}
}

// WithDecoderConcurrency sets the zstd decoder concurrency (default: 1).
// Values < 0 are treated as 0 (use GOMAXPROCS).
func WithDecoderConcurrency(n int) Option {
	return func(b *Blob) {
		if n < 0 {
			n = 0
		}
		b.decoderConcurrency = n
		b.decoderConcurrencySet = true
	}
}

// WithDecoderLowmem sets whether the zstd decoder should use low-memory mode (default: false).
func WithDecoderLowmem(enabled bool) Option {
	return func(b *Blob) {
		b.decoderLowmem = enabled
		b.decoderLowmemSet = true
	}
}

// WithVerifyOnClose controls whether Close drains the file to verify the hash.
//
// When false, Close returns without reading the remaining data. Integrity is
// only guaranteed when callers read to EOF.
func WithVerifyOnClose(enabled bool) Option {
	return func(b *Blob) {
		b.verifyOnClose = enabled
	}
}

// WithCache enables content-addressed caching.
//
// When enabled, file content is cached after first read and served from cache
// on subsequent reads. Concurrent requests for the same content are deduplicated.
func WithCache(c cache.Cache) Option {
	return func(b *Blob) {
		b.cache = c
	}
}

// CopyOption configures CopyTo and CopyDir operations.
type CopyOption func(*copyConfig)

// defaultCopyReadConcurrency is used when no CopyWithReadConcurrency option is set.
const defaultCopyReadConcurrency = 4

type copyConfig struct {
	overwrite          bool
	preserveMode       bool
	preserveTimes      bool
	workers            int
	readConcurrency    int
	readConcurrencySet bool
	readAheadBytes     uint64
	readAheadBytesSet  bool
	cleanDest          bool
}

// CopyWithOverwrite allows overwriting existing files.
// By default, existing files are skipped.
func CopyWithOverwrite(overwrite bool) CopyOption {
	return func(c *copyConfig) {
		c.overwrite = overwrite
	}
}

// CopyWithPreserveMode preserves file permission modes from the archive.
// By default, modes are not preserved (files use umask defaults).
func CopyWithPreserveMode(preserve bool) CopyOption {
	return func(c *copyConfig) {
		c.preserveMode = preserve
	}
}

// CopyWithPreserveTimes preserves file modification times from the archive.
// By default, times are not preserved (files use current time).
func CopyWithPreserveTimes(preserve bool) CopyOption {
	return func(c *copyConfig) {
		c.preserveTimes = preserve
	}
}

// CopyWithCleanDest clears the destination prefix before copying and writes
// directly to the final path (no temp files). This is only supported by CopyDir.
func CopyWithCleanDest(enabled bool) CopyOption {
	return func(c *copyConfig) {
		c.cleanDest = enabled
	}
}

// CopyWithWorkers sets the number of workers for parallel processing.
// Values < 0 force serial processing. Zero uses automatic heuristics.
// Values > 0 force a specific worker count.
func CopyWithWorkers(n int) CopyOption {
	return func(c *copyConfig) {
		c.workers = n
	}
}

// CopyWithReadConcurrency sets the number of concurrent range reads.
// Use 1 to force serial reads. Zero uses the default concurrency (4).
func CopyWithReadConcurrency(n int) CopyOption {
	return func(c *copyConfig) {
		if n <= 0 {
			n = defaultCopyReadConcurrency
		}
		c.readConcurrency = n
		c.readConcurrencySet = true
	}
}

// CopyWithReadAheadBytes caps the total size of buffered group data.
// A value of 0 disables the byte budget.
func CopyWithReadAheadBytes(limit uint64) CopyOption {
	return func(c *copyConfig) {
		c.readAheadBytes = limit
		c.readAheadBytesSet = true
	}
}
