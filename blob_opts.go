package blob

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

// WithVerifyOnClose controls whether Close drains the file to verify the hash.
//
// When false, Close returns without reading the remaining data. Integrity is
// only guaranteed when callers read to EOF.
func WithVerifyOnClose(enabled bool) Option {
	return func(b *Blob) {
		b.verifyOnClose = enabled
	}
}

// CopyOption configures CopyTo and CopyDir operations.
type CopyOption func(*copyConfig)

type copyConfig struct {
	overwrite     bool
	preserveMode  bool
	preserveTimes bool
	workers       int
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

// CopyWithWorkers sets the number of workers for parallel processing.
// Values < 0 force serial processing. Zero uses automatic heuristics.
// Values > 0 force a specific worker count.
func CopyWithWorkers(n int) CopyOption {
	return func(c *copyConfig) {
		c.workers = n
	}
}
