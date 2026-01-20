package disk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"

	blobcache "github.com/meigma/blob/core/cache"
)

// BlockCache provides a disk-backed block cache for ByteSources.
// Blocks are stored as individual files in a directory hierarchy with optional
// sharding by key prefix. The cache is safe for concurrent use.
type BlockCache struct {
	dir            string             // root directory for cached blocks
	shardPrefixLen int                // number of hex chars for subdirectory sharding
	dirPerm        os.FileMode        // permissions for created directories
	maxBytes       int64              // maximum cache size (0 = unlimited)
	bytes          atomic.Int64       // current total size of cached blocks
	fetchGroup     singleflight.Group // deduplicates concurrent fetches for same block
	pruneMu        sync.Mutex         // serializes prune operations
}

// BlockCacheOption configures a disk-backed block cache.
type BlockCacheOption func(*BlockCache)

// WithBlockMaxBytes sets the maximum size in bytes for the block cache.
// Values <= 0 disable the limit.
func WithBlockMaxBytes(n int64) BlockCacheOption {
	return func(c *BlockCache) {
		c.maxBytes = n
	}
}

// WithBlockShardPrefixLen sets the number of hex characters used for sharding.
// Use 0 to disable sharding. Defaults to 2.
func WithBlockShardPrefixLen(n int) BlockCacheOption {
	return func(c *BlockCache) {
		c.shardPrefixLen = n
	}
}

// WithBlockDirPerm sets the directory permissions used for cache directories.
func WithBlockDirPerm(mode os.FileMode) BlockCacheOption {
	return func(c *BlockCache) {
		c.dirPerm = mode
	}
}

// NewBlockCache creates a disk-backed block cache rooted at dir.
func NewBlockCache(dir string, opts ...BlockCacheOption) (*BlockCache, error) {
	if dir == "" {
		return nil, errors.New("block cache dir is empty")
	}
	c := &BlockCache{
		dir:            dir,
		shardPrefixLen: defaultShardPrefixLen,
		dirPerm:        defaultDirPerm,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.shardPrefixLen < 0 {
		return nil, errors.New("block cache shard prefix length must be >= 0")
	}
	if c.maxBytes < 0 {
		return nil, errors.New("block cache max bytes must be >= 0")
	}
	if err := os.MkdirAll(dir, c.dirPerm); err != nil {
		return nil, err
	}
	if size, err := dirSize(dir); err == nil {
		c.bytes.Store(size)
	} else {
		return nil, err
	}
	return c, nil
}

// Wrap returns a ByteSource that caches reads in fixed-size blocks.
func (c *BlockCache) Wrap(src blobcache.ByteSource, opts ...blobcache.WrapOption) (blobcache.ByteSource, error) {
	if src == nil {
		return nil, errors.New("block cache: source is nil")
	}
	cfg := blobcache.DefaultWrapConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.BlockSize <= 0 {
		return nil, errors.New("block cache: block size must be > 0")
	}
	if cfg.BlockSize > math.MaxInt {
		return nil, errors.New("block cache: block size exceeds max int")
	}
	if cfg.MaxBlocksPerRead < 0 {
		return nil, errors.New("block cache: max blocks per read must be >= 0")
	}
	sourceID := src.SourceID()
	if sourceID == "" {
		return nil, errors.New("block cache: source id is empty")
	}
	return &cachedSource{
		src:              src,
		cache:            c,
		sourceID:         sourceID,
		blockSize:        cfg.BlockSize,
		maxBlocksPerRead: cfg.MaxBlocksPerRead,
	}, nil
}

// MaxBytes returns the configured cache size limit (0 = unlimited).
func (c *BlockCache) MaxBytes() int64 {
	return c.maxBytes
}

// SizeBytes returns the current cache size in bytes.
func (c *BlockCache) SizeBytes() int64 {
	return c.bytes.Load()
}

// Prune removes cached entries until the cache is at or below targetBytes.
func (c *BlockCache) Prune(targetBytes int64) (int64, error) {
	if targetBytes < 0 {
		targetBytes = 0
	}
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()

	freed, remaining, err := pruneDir(c.dir, targetBytes)
	if err != nil {
		return 0, err
	}
	c.bytes.Store(remaining)
	return freed, nil
}

// cachedSource wraps a ByteSource with block-level caching.
type cachedSource struct {
	src              blobcache.ByteSource
	cache            *BlockCache
	sourceID         string
	blockSize        int64
	maxBlocksPerRead int
}

func (s *cachedSource) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("read at %d: negative offset", off)
	}
	size := s.src.Size()
	if off >= size {
		return 0, io.EOF
	}

	expected := int64(len(p))
	if off+expected > size {
		expected = size - off
	}

	startBlock := off / s.blockSize
	endBlock := (off + expected - 1) / s.blockSize
	blockCount := endBlock - startBlock + 1

	if s.maxBlocksPerRead > 0 && blockCount > int64(s.maxBlocksPerRead) {
		return s.src.ReadAt(p, off)
	}

	var n int64
	for blockIndex := startBlock; blockIndex <= endBlock; blockIndex++ {
		blockStart := blockIndex * s.blockSize
		blockEnd := blockStart + s.blockSize
		if blockEnd > size {
			blockEnd = size
		}
		blockLen := blockEnd - blockStart

		data, err := s.cache.getBlock(s.sourceID, s.blockSize, blockIndex, blockLen, func() ([]byte, error) {
			return s.readBlockFromSource(blockStart, blockLen)
		})
		if err != nil {
			return int(n), err
		}
		if int64(len(data)) < blockLen {
			return int(n), io.ErrUnexpectedEOF
		}

		copyStart := max(off, blockStart)
		copyEnd := min(off+expected, blockEnd)
		srcOffset := copyStart - blockStart
		dstOffset := copyStart - off
		length := copyEnd - copyStart

		if length > 0 {
			copy(p[dstOffset:dstOffset+length], data[srcOffset:srcOffset+length])
			n += length
		}
	}

	if expected < int64(len(p)) {
		return int(n), io.EOF
	}
	return int(n), nil
}

func (s *cachedSource) ReadRange(off, length int64) (io.ReadCloser, error) {
	if length < 0 {
		return nil, fmt.Errorf("read range length %d: negative length", length)
	}
	if length == 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if off < 0 {
		return nil, fmt.Errorf("read range %d: negative offset", off)
	}
	size := s.src.Size()
	if off >= size {
		return io.NopCloser(bytes.NewReader(nil)), io.EOF
	}
	if length > size-off {
		length = size - off
	}
	return io.NopCloser(io.NewSectionReader(s, off, length)), nil
}

func (s *cachedSource) Size() int64 {
	return s.src.Size()
}

func (s *cachedSource) SourceID() string {
	return s.sourceID
}

func (s *cachedSource) readBlockFromSource(off, length int64) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}
	if rr, ok := s.src.(blobcache.RangeReader); ok {
		rc, err := rr.ReadRange(off, length)
		if err != nil {
			return nil, err
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != length {
			return nil, io.ErrUnexpectedEOF
		}
		return data, nil
	}

	if length > math.MaxInt {
		return nil, errors.New("block cache: block length exceeds max int")
	}

	buf := make([]byte, int(length))
	n, err := s.src.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if int64(n) != length {
		return nil, io.ErrUnexpectedEOF
	}
	return buf, nil
}

func (c *BlockCache) getBlock(sourceID string, blockSize, blockIndex, blockLen int64, fetch func() ([]byte, error)) ([]byte, error) {
	key := c.blockKeyHex(sourceID, blockSize, blockIndex)
	result, err, _ := c.fetchGroup.Do(key, func() (any, error) {
		path := c.pathForKey(key)
		// path is safe: constructed from hex-encoded SHA256 hash via pathForKey
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // path is derived from hash, not user input
			if int64(len(data)) == blockLen {
				return data, nil
			}
			c.bytes.Add(-int64(len(data)))
			_ = os.Remove(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		data, err := fetch()
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != blockLen {
			return nil, io.ErrUnexpectedEOF
		}
		// Ignore write errors - we still return the fetched data even if caching fails.
		// This is intentional: cache writes are opportunistic and should not fail the read.
		_ = c.writeBlock(path, data) //nolint:errcheck // cache write is best-effort
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]byte), nil //nolint:errcheck // type assertion always succeeds when err is nil
}

func (c *BlockCache) writeBlock(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	if ok, err := c.ensureCapacity(int64(len(data))); err != nil {
		return err
	} else if !ok {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, c.dirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "block-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = os.Remove(tmpPath)
			return nil
		}
		_ = os.Remove(tmpPath)
		return err
	}
	c.bytes.Add(int64(len(data)))
	return nil
}

func (c *BlockCache) blockKeyHex(sourceID string, blockSize, blockIndex int64) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(sourceID)) //nolint:errcheck // hash writes never fail

	var buf [16]byte
	// blockSize and blockIndex are validated positive before reaching here
	binary.BigEndian.PutUint64(buf[:8], uint64(blockSize))  //nolint:gosec // blockSize validated > 0
	binary.BigEndian.PutUint64(buf[8:], uint64(blockIndex)) //nolint:gosec // blockIndex always >= 0
	_, _ = hasher.Write(buf[:])                             //nolint:errcheck // hash writes never fail

	return hex.EncodeToString(hasher.Sum(nil))
}

func (c *BlockCache) pathForKey(hexKey string) string {
	if c.shardPrefixLen <= 0 {
		return filepath.Join(c.dir, hexKey)
	}
	prefixLen := c.shardPrefixLen
	if prefixLen > len(hexKey) {
		prefixLen = len(hexKey)
	}
	return filepath.Join(c.dir, hexKey[:prefixLen], hexKey)
}

func (c *BlockCache) ensureCapacity(need int64) (bool, error) {
	if c.maxBytes <= 0 {
		return true, nil
	}
	if need > c.maxBytes {
		return false, nil
	}
	if c.SizeBytes()+need <= c.maxBytes {
		return true, nil
	}
	if _, err := c.Prune(c.maxBytes - need); err != nil {
		return false, err
	}
	return c.SizeBytes()+need <= c.maxBytes, nil
}
