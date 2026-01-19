package blob

import "github.com/meigma/blob/core/internal/write"

// ChangeDetection controls how strictly file changes are detected during creation.
type ChangeDetection uint8

// SkipCompressionFunc returns true when a file should be stored uncompressed.
// It is called once per file and should be inexpensive.
type SkipCompressionFunc = write.SkipCompressionFunc

// DefaultSkipCompression returns a SkipCompressionFunc that skips small files
// and known already-compressed extensions.
var DefaultSkipCompression = write.DefaultSkipCompression

const (
	ChangeDetectionNone ChangeDetection = iota
	ChangeDetectionStrict
)

// createConfig holds configuration for archive creation.
type createConfig struct {
	compression     Compression
	changeDetection ChangeDetection
	skipCompression []SkipCompressionFunc
	maxFiles        int
}

// CreateOption configures archive creation.
type CreateOption func(*createConfig)

// CreateWithCompression sets the compression algorithm to use.
// Use CompressionNone to store files uncompressed, CompressionZstd for zstd.
func CreateWithCompression(c Compression) CreateOption {
	return func(cfg *createConfig) {
		cfg.compression = c
	}
}

// CreateWithChangeDetection controls whether the writer verifies files did not change
// during archive creation. The zero value disables change detection to reduce
// syscalls; enable ChangeDetectionStrict for stronger guarantees.
func CreateWithChangeDetection(cd ChangeDetection) CreateOption {
	return func(cfg *createConfig) {
		cfg.changeDetection = cd
	}
}

// CreateWithSkipCompression adds predicates that decide to store a file uncompressed.
// If any predicate returns true, compression is skipped for that file.
// These checks are on the hot path, so keep them cheap.
func CreateWithSkipCompression(fns ...SkipCompressionFunc) CreateOption {
	return func(cfg *createConfig) {
		cfg.skipCompression = append(cfg.skipCompression, fns...)
	}
}

// CreateWithMaxFiles limits the number of files included in the archive.
// Zero uses DefaultMaxFiles. Negative means no limit.
func CreateWithMaxFiles(n int) CreateOption {
	return func(cfg *createConfig) {
		cfg.maxFiles = n
	}
}
