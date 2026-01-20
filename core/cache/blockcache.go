package cache

import "io"

// ByteSource provides random access to data for block caching.
type ByteSource interface {
	io.ReaderAt

	// Size returns the total size of the data source in bytes.
	Size() int64

	// SourceID returns a unique identifier for this data source.
	// The ID is used as part of the cache key, so it must be stable
	// across calls and unique across different sources.
	SourceID() string
}

// RangeReader provides range reads for block cache fetches.
// Types implementing both ByteSource and RangeReader allow the block cache
// to use more efficient range-based fetching instead of ReadAt.
type RangeReader interface {
	// ReadRange returns a ReadCloser for reading length bytes starting at off.
	// The caller is responsible for closing the returned ReadCloser.
	ReadRange(off, length int64) (io.ReadCloser, error)
}

// BlockCache wraps ByteSources with block-level caching.
//
// Block caching is most effective for random, non-contiguous reads (e.g. scattered
// ReadFile/Open calls over remote sources). For large sequential reads (CopyDir/CopyTo),
// caching can add overhead; DefaultMaxBlocksPerRead provides a conservative bypass
// to avoid caching large ranges.
type BlockCache interface {
	// Wrap returns a ByteSource that caches reads from src in fixed-size blocks.
	// The returned ByteSource also implements RangeReader if the underlying
	// cache supports it.
	Wrap(src ByteSource, opts ...WrapOption) (ByteSource, error)

	// MaxBytes returns the configured cache size limit (0 = unlimited).
	MaxBytes() int64

	// SizeBytes returns the current cache size in bytes.
	SizeBytes() int64

	// Prune removes cached entries until the cache is at or below targetBytes.
	// Returns the number of bytes freed.
	Prune(targetBytes int64) (int64, error)
}

// DefaultBlockSize is the default block size used by block caches.
const DefaultBlockSize int64 = 64 << 10

// DefaultMaxBlocksPerRead caps cached blocks per ReadAt to avoid large sequential reads.
const DefaultMaxBlocksPerRead = 4

// WrapConfig controls block cache wrapping behavior.
type WrapConfig struct {
	// BlockSize is the size in bytes of each cached block.
	// Smaller blocks improve cache hit rates for random reads but increase
	// metadata overhead. Larger blocks are more efficient for sequential reads.
	BlockSize int64

	// MaxBlocksPerRead is the maximum number of blocks that will be cached
	// for a single ReadAt call. Reads spanning more blocks than this limit
	// bypass the cache entirely. Use 0 to disable the limit.
	MaxBlocksPerRead int
}

// DefaultWrapConfig returns the default block cache configuration.
func DefaultWrapConfig() WrapConfig {
	return WrapConfig{
		BlockSize:        DefaultBlockSize,
		MaxBlocksPerRead: DefaultMaxBlocksPerRead,
	}
}

// WrapOption configures block cache wrapping behavior.
type WrapOption func(*WrapConfig)

// WithBlockSize sets the block size used for caching.
func WithBlockSize(n int64) WrapOption {
	return func(cfg *WrapConfig) {
		cfg.BlockSize = n
	}
}

// WithMaxBlocksPerRead bypasses caching when a ReadAt spans more than n blocks.
// Values <= 0 disable the limit.
func WithMaxBlocksPerRead(n int) WrapOption {
	return func(cfg *WrapConfig) {
		cfg.MaxBlocksPerRead = n
	}
}
