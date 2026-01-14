package fileops

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/meigma/blob/internal/sizing"
)

const (
	// DefaultMaxFileSize is the default maximum file size (256MB).
	DefaultMaxFileSize = 256 << 20

	// DefaultMaxDecoderMemory is the default maximum decoder memory (256MB).
	DefaultMaxDecoderMemory = 256 << 20
)

// ByteSource provides random access to the data.
type ByteSource interface {
	io.ReaderAt
	Size() int64
}

// Ops handles reading and verifying file content from a ByteSource.
type Ops struct {
	source           ByteSource
	maxFileSize      uint64
	maxDecoderMemory uint64
	pool             *DecompressPool
}

// Option configures an Ops instance.
type Option func(*Ops)

// WithMaxFileSize sets the maximum file size limit.
// Set to 0 to disable the limit.
func WithMaxFileSize(limit uint64) Option {
	return func(o *Ops) {
		o.maxFileSize = limit
	}
}

// WithMaxDecoderMemory sets the maximum decoder memory limit.
// Set to 0 to disable the limit.
func WithMaxDecoderMemory(limit uint64) Option {
	return func(o *Ops) {
		o.maxDecoderMemory = limit
	}
}

// New creates a new Ops instance for reading files from the given source.
func New(source ByteSource, opts ...Option) *Ops {
	o := &Ops{
		source:           source,
		maxFileSize:      DefaultMaxFileSize,
		maxDecoderMemory: DefaultMaxDecoderMemory,
	}
	for _, opt := range opts {
		opt(o)
	}
	o.pool = NewDecompressPool(o.maxDecoderMemory)
	return o
}

// ReadAll reads the entire content of an entry, decompresses if needed,
// and verifies the hash. Returns the uncompressed content.
func (o *Ops) ReadAll(entry *Entry) ([]byte, error) {
	if err := ValidateAll(entry, o.source.Size(), o.maxFileSize); err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}

	section, err := o.sectionReader(entry)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.Path, err)
	}

	reader, release, err := o.entryReader(entry, section)
	if err != nil {
		return nil, err
	}
	defer release()

	content, sum, err := o.readContentAndHash(entry, reader)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(sum, entry.Hash) {
		return nil, ErrHashMismatch
	}

	return content, nil
}

// Source returns the underlying ByteSource.
func (o *Ops) Source() ByteSource {
	return o.source
}

// MaxFileSize returns the configured maximum file size.
func (o *Ops) MaxFileSize() uint64 {
	return o.maxFileSize
}

// Pool returns the decompression pool for reuse.
func (o *Ops) Pool() *DecompressPool {
	return o.pool
}

// sectionReader creates a bounded section reader for an entry.
func (o *Ops) sectionReader(entry *Entry) (*io.SectionReader, error) {
	offset, err := sizing.ToInt64(entry.DataOffset, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	length, err := sizing.ToInt64(entry.DataSize, ErrSizeOverflow)
	if err != nil {
		return nil, err
	}
	return io.NewSectionReader(o.source, offset, length), nil
}

// entryReader creates the appropriate reader for an entry based on compression.
func (o *Ops) entryReader(entry *Entry, section *io.SectionReader) (io.Reader, func(), error) {
	switch entry.Compression {
	case CompressionNone:
		return section, func() {}, nil
	case CompressionZstd:
		dec, release, err := o.pool.Get(section)
		if err != nil {
			return nil, func() {}, fmt.Errorf("%w: %v", ErrDecompression, err)
		}
		return dec, release, nil
	default:
		return nil, func() {}, fmt.Errorf("unknown compression algorithm: %d", entry.Compression)
	}
}

// readContentAndHash reads content and computes its hash.
func (o *Ops) readContentAndHash(entry *Entry, reader io.Reader) (content, sum []byte, err error) {
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
