package file

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/meigma/blob/core/internal/sizing"
)

const (
	// DefaultMaxFileSize is the default maximum file size (256MB).
	DefaultMaxFileSize = 256 << 20

	// DefaultMaxDecoderMemory is the default maximum decoder memory (256MB).
	DefaultMaxDecoderMemory = 256 << 20
)

// ByteSource provides random access to the data.
// SourceID must return a stable identifier for the underlying content.
type ByteSource interface {
	io.ReaderAt
	Size() int64
	SourceID() string
}

type rangeReader interface {
	ReadRange(off, length int64) (io.ReadCloser, error)
}

// Reader reads and verifies file content from a ByteSource.
type Reader struct {
	source                ByteSource
	maxFileSize           uint64
	maxDecoderMemory      uint64
	decoderConcurrencySet bool
	decoderConcurrency    int
	decoderLowmemSet      bool
	decoderLowmem         bool
	pool                  *DecompressPool
}

// Option configures a Reader.
type Option func(*Reader)

// WithMaxFileSize sets the maximum file size limit.
// Set to 0 to disable the limit.
func WithMaxFileSize(limit uint64) Option {
	return func(r *Reader) {
		r.maxFileSize = limit
	}
}

// WithMaxDecoderMemory sets the maximum decoder memory limit.
// Set to 0 to disable the limit.
func WithMaxDecoderMemory(limit uint64) Option {
	return func(r *Reader) {
		r.maxDecoderMemory = limit
	}
}

// WithDecoderConcurrency sets the zstd decoder concurrency (default: 1).
// Values < 0 are treated as 0 (use GOMAXPROCS).
func WithDecoderConcurrency(n int) Option {
	return func(r *Reader) {
		if n < 0 {
			n = 0
		}
		r.decoderConcurrency = n
		r.decoderConcurrencySet = true
	}
}

// WithDecoderLowmem sets whether the zstd decoder should use low-memory mode (default: false).
func WithDecoderLowmem(enabled bool) Option {
	return func(r *Reader) {
		r.decoderLowmem = enabled
		r.decoderLowmemSet = true
	}
}

// NewReader creates a Reader for reading files from the given source.
func NewReader(source ByteSource, opts ...Option) *Reader {
	r := &Reader{
		source:           source,
		maxFileSize:      DefaultMaxFileSize,
		maxDecoderMemory: DefaultMaxDecoderMemory,
	}
	for _, opt := range opts {
		opt(r)
	}
	poolOpts := make([]decompressOption, 0, 2)
	if r.decoderConcurrencySet {
		poolOpts = append(poolOpts, withDecoderConcurrency(r.decoderConcurrency))
	}
	if r.decoderLowmemSet {
		poolOpts = append(poolOpts, withDecoderLowmem(r.decoderLowmem))
	}
	r.pool = NewDecompressPool(r.maxDecoderMemory, poolOpts...)
	return r
}

// ReadAll reads the entire content of an entry, decompresses if needed,
// and verifies the hash. Returns the uncompressed content.
func (r *Reader) ReadAll(entry *Entry) ([]byte, error) {
	if err := ValidateAll(entry, r.source.Size(), r.maxFileSize); err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}

	section, err := r.sectionReader(entry)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}

	reader, release, err := r.entryReader(entry, section)
	if err != nil {
		return nil, err
	}
	defer release()

	content, sum, err := r.readContentAndHash(entry, reader)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(sum, entry.Hash) {
		return nil, ErrHashMismatch
	}

	return content, nil
}

// Source returns the underlying ByteSource.
func (r *Reader) Source() ByteSource {
	return r.source
}

// MaxFileSize returns the configured maximum file size.
func (r *Reader) MaxFileSize() uint64 {
	return r.maxFileSize
}

// Pool returns the decompression pool for reuse.
func (r *Reader) Pool() *DecompressPool {
	return r.pool
}

// sectionReader creates a bounded section reader for an entry.
func (r *Reader) sectionReader(entry *Entry) (*io.SectionReader, error) {
	offset, err := sizing.ToInt64(entry.DataOffset, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	length, err := sizing.ToInt64(entry.DataSize, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	return io.NewSectionReader(r.source, offset, length), nil
}

// entryReader creates the appropriate reader for an entry based on compression.
func (r *Reader) entryReader(entry *Entry, section *io.SectionReader) (io.Reader, func(), error) {
	switch entry.Compression {
	case CompressionNone:
		return section, func() {}, nil
	case CompressionZstd:
		if rr, ok := r.source.(rangeReader); ok {
			reader, err := r.rangeReader(entry, rr)
			if err != nil {
				return nil, func() {}, fmt.Errorf("%w: %v", ErrDecompression, err)
			}
			dec, release, err := r.pool.Get(reader)
			if err != nil {
				_ = reader.Close()
				return nil, func() {}, fmt.Errorf("%w: %v", ErrDecompression, err)
			}
			return dec, func() {
				release()
				_ = reader.Close()
			}, nil
		}
		dec, release, err := r.pool.Get(section)
		if err != nil {
			return nil, func() {}, fmt.Errorf("%w: %v", ErrDecompression, err)
		}
		return dec, release, nil
	default:
		return nil, func() {}, fmt.Errorf("unknown compression algorithm: %d", entry.Compression)
	}
}

func (r *Reader) rangeReader(entry *Entry, rr rangeReader) (io.ReadCloser, error) {
	offset, err := sizing.ToInt64(entry.DataOffset, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	length, err := sizing.ToInt64(entry.DataSize, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return rr.ReadRange(offset, length)
}

// readContentAndHash reads content and computes its hash.
func (r *Reader) readContentAndHash(entry *Entry, reader io.Reader) (content, sum []byte, err error) {
	contentSize, err := sizing.ToInt(entry.OriginalSize, ErrSizeOverflow)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}
	content = make([]byte, contentSize)

	hr := NewHashingReader(reader, sha256.New())
	n, err := io.ReadFull(hr, content)
	if err != nil {
		return nil, nil, mapReadError(entry, n, contentSize, err)
	}
	if err := EnsureNoExtra(hr); err != nil {
		return nil, nil, err
	}

	return content, hr.Sum(), nil
}

// mapReadError converts read errors to appropriate error types.
func mapReadError(entry *Entry, n, expected int, err error) error {
	if entry.Compression == CompressionNone {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("read %s: short read (%d of %d bytes)", entry.Path, n, expected)
		}
		return fmt.Errorf("read %s: %w", entry.Path, err)
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return fmt.Errorf("%w: unexpected EOF", ErrDecompression)
	}
	return fmt.Errorf("%w: %v", ErrDecompression, err)
}
