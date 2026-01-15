package batch

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/fileops"
	"github.com/meigma/blob/internal/sizing"
)

// Re-export compression constants for use in filesink.
const (
	CompressionNone = blobtype.CompressionNone
	CompressionZstd = blobtype.CompressionZstd
)

// Compression is an alias for blobtype.Compression.
type Compression = blobtype.Compression

const (
	// parallelMinAvgBytes is the minimum average entry size to use parallel processing.
	// Below this threshold, serial processing is more efficient due to reduced overhead.
	parallelMinAvgBytes = 64 << 10 // 64KB
)

// Processor handles batch reading and processing of entries from a blob archive.
//
// It provides efficient reading by grouping adjacent entries and processing them
// together, minimizing the number of read operations on the underlying source.
type Processor struct {
	source      fileops.ByteSource
	pool        *fileops.DecompressPool
	maxFileSize uint64
	workers     int // 0 = auto, <0 = serial, >0 = fixed count
}

// ProcessorOption configures a Processor.
type ProcessorOption func(*Processor)

// WithWorkers sets the number of workers for parallel processing.
// Values < 0 force serial processing. Zero uses automatic heuristics.
// Values > 0 force a specific worker count.
func WithWorkers(n int) ProcessorOption {
	return func(p *Processor) {
		p.workers = n
	}
}

// NewProcessor creates a new batch processor.
//
// The source provides random access to the data blob.
// The pool provides reusable zstd decoders for compressed entries.
// maxFileSize limits the size of individual entries (0 for no limit).
func NewProcessor(source fileops.ByteSource, pool *fileops.DecompressPool, maxFileSize uint64, opts ...ProcessorOption) *Processor {
	p := &Processor{
		source:      source,
		pool:        pool,
		maxFileSize: maxFileSize,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Process reads and processes entries, writing results to the sink.
//
// Entries are filtered through sink.ShouldProcess, sorted by offset,
// grouped into contiguous ranges, and processed efficiently. For each
// entry, the content is decompressed (if needed), hash-verified, and
// written to the sink.
//
// Processing stops on the first error encountered.
func (p *Processor) Process(entries []*Entry, sink Sink) error {
	if len(entries) == 0 {
		return nil
	}

	// Filter entries that should be processed
	toProcess := make([]*Entry, 0, len(entries))
	for _, entry := range entries {
		if sink.ShouldProcess(entry) {
			toProcess = append(toProcess, entry)
		}
	}
	if len(toProcess) == 0 {
		return nil
	}

	// Validate all entries
	sourceSize := p.source.Size()
	for _, entry := range toProcess {
		if err := fileops.ValidateAll(entry, sourceSize, p.maxFileSize); err != nil {
			return fmt.Errorf("batch: %s: %w", entry.Path, err)
		}
	}

	// Sort by data offset for efficient grouping
	slices.SortFunc(toProcess, func(a, b *Entry) int {
		if a.DataOffset < b.DataOffset {
			return -1
		}
		if a.DataOffset > b.DataOffset {
			return 1
		}
		return 0
	})

	// Group adjacent entries and process each group
	groups := groupAdjacentEntries(toProcess)
	for _, group := range groups {
		if err := p.processGroup(group, sink); err != nil {
			return err
		}
	}
	return nil
}

// processGroup reads a contiguous range and processes each entry.
func (p *Processor) processGroup(group rangeGroup, sink Sink) error {
	data, err := p.readGroupData(group)
	if err != nil {
		return err
	}
	if len(group.entries) == 0 {
		return nil
	}

	workers := p.workerCount(group.entries)
	if workers < 2 {
		return p.processEntriesSerial(group.entries, data, group.start, sink)
	}
	return p.processEntriesParallel(group.entries, data, group.start, sink, workers)
}

// readGroupData reads the contiguous byte range for a group.
func (p *Processor) readGroupData(group rangeGroup) ([]byte, error) {
	size := group.end - group.start
	sizeInt, err := sizing.ToInt(size, blobtype.ErrSizeOverflow)
	if err != nil {
		return nil, fmt.Errorf("batch: %w", err)
	}
	data := make([]byte, sizeInt)
	n, err := p.source.ReadAt(data, int64(group.start)) //nolint:gosec // offset fits in int64 after validation
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("batch: %w", err)
	}
	if uint64(n) != size { //nolint:gosec // n is always non-negative
		return nil, fmt.Errorf("batch: short read (%d of %d bytes)", n, size)
	}
	return data, nil
}

// processEntriesSerial processes entries one at a time.
func (p *Processor) processEntriesSerial(entries []*Entry, data []byte, groupStart uint64, sink Sink) error {
	for _, entry := range entries {
		if err := p.processEntry(entry, data, groupStart, sink); err != nil {
			return err
		}
	}
	return nil
}

// processEntriesParallel processes entries concurrently.
func (p *Processor) processEntriesParallel(entries []*Entry, data []byte, groupStart uint64, sink Sink, workers int) error {
	var stop atomic.Bool
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for w := range workers {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for i := start; i < len(entries); i += workers {
				if stop.Load() {
					return
				}
				if err := p.processEntry(entries[i], data, groupStart, sink); err != nil {
					if stop.CompareAndSwap(false, true) {
						errCh <- err
					}
					return
				}
			}
		}(w)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// processEntry decompresses, verifies, and writes a single entry.
func (p *Processor) processEntry(entry *Entry, groupData []byte, groupStart uint64, sink Sink) error {
	// Extract this entry's data from the group
	localOffset := entry.DataOffset - groupStart
	localEnd := localOffset + entry.DataSize
	if localEnd < localOffset || localEnd > uint64(len(groupData)) {
		return blobtype.ErrSizeOverflow
	}
	start, err := sizing.ToInt(localOffset, blobtype.ErrSizeOverflow)
	if err != nil {
		return err
	}
	end, err := sizing.ToInt(localEnd, blobtype.ErrSizeOverflow)
	if err != nil {
		return err
	}
	entryData := groupData[start:end]

	if bufferedSink, ok := sink.(BufferedSink); ok {
		content, err := p.decompress(entry, entryData)
		if err != nil {
			return fmt.Errorf("batch: %s: %w", entry.Path, err)
		}
		sum := sha256.Sum256(content)
		if !bytes.Equal(sum[:], entry.Hash) {
			return fmt.Errorf("batch: %s: %w", entry.Path, blobtype.ErrHashMismatch)
		}
		if err := bufferedSink.PutBuffered(entry, content); err != nil {
			return fmt.Errorf("batch: %s: %w", entry.Path, err)
		}
		return nil
	}

	// Get writer from sink
	w, err := sink.Writer(entry)
	if err != nil {
		return fmt.Errorf("batch: %s: %w", entry.Path, err)
	}

	var processErr error
	if entry.Compression == blobtype.CompressionNone {
		processErr = p.writeVerifyUncompressed(entry, entryData, w)
	} else {
		processErr = p.streamDecompressVerify(entry, entryData, w)
	}
	if processErr != nil {
		_ = w.Discard() //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("batch: %s: %w", entry.Path, processErr)
	}

	if err := w.Commit(); err != nil {
		return fmt.Errorf("batch: %s: commit: %w", entry.Path, err)
	}
	return nil
}

func (p *Processor) writeVerifyUncompressed(entry *Entry, data []byte, w io.Writer) error {
	if err := p.verifyUncompressed(entry, data); err != nil {
		return err
	}
	return writeAll(w, data)
}

func (p *Processor) verifyUncompressed(entry *Entry, data []byte) error {
	if uint64(len(data)) != entry.OriginalSize {
		return fmt.Errorf("%w: size mismatch", blobtype.ErrDecompression)
	}

	sum := sha256.Sum256(data)
	if !bytes.Equal(sum[:], entry.Hash) {
		return blobtype.ErrHashMismatch
	}

	return nil
}

func (p *Processor) decompress(entry *Entry, data []byte) ([]byte, error) {
	switch entry.Compression {
	case blobtype.CompressionNone:
		if uint64(len(data)) != entry.OriginalSize {
			return nil, fmt.Errorf("%w: size mismatch", blobtype.ErrDecompression)
		}
		return data, nil
	case blobtype.CompressionZstd:
		contentSize, err := sizing.ToInt(entry.OriginalSize, blobtype.ErrSizeOverflow)
		if err != nil {
			return nil, err
		}
		dec, closeFn, err := p.pool.Get(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", blobtype.ErrDecompression, err)
		}
		defer closeFn()

		content := make([]byte, contentSize)
		if _, err := io.ReadFull(dec, content); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("%w: unexpected EOF", blobtype.ErrDecompression)
			}
			return nil, fmt.Errorf("%w: %v", blobtype.ErrDecompression, err)
		}
		if err := fileops.EnsureNoExtra(dec); err != nil {
			return nil, err
		}
		return content, nil
	default:
		return nil, fmt.Errorf("unknown compression algorithm: %d", entry.Compression)
	}
}

// streamDecompressVerify decompresses entry data, verifies hash, and writes to w.
func (p *Processor) streamDecompressVerify(entry *Entry, data []byte, w io.Writer) error {
	reader, closeFn, err := p.newEntryReader(entry, data)
	if err != nil {
		return err
	}
	defer closeFn()

	hasher := sha256.New()
	tee := io.TeeReader(reader, hasher)

	expected, err := sizing.ToInt64(entry.OriginalSize, blobtype.ErrSizeOverflow)
	if err != nil {
		return err
	}

	// Copy exactly OriginalSize bytes through hasher to writer
	if _, err := io.CopyN(w, tee, expected); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("%w: unexpected EOF", blobtype.ErrDecompression)
		}
		return err
	}

	// Verify no extra data
	if err := fileops.EnsureNoExtra(tee); err != nil {
		return err
	}

	// Verify hash
	sum := hasher.Sum(nil)
	if !bytes.Equal(sum, entry.Hash) {
		return blobtype.ErrHashMismatch
	}
	return nil
}

// newEntryReader creates a reader for the entry's compressed data.
func (p *Processor) newEntryReader(entry *Entry, data []byte) (io.Reader, func(), error) {
	switch entry.Compression {
	case blobtype.CompressionNone:
		return bytes.NewReader(data), func() {}, nil
	case blobtype.CompressionZstd:
		dec, closeFn, err := p.pool.Get(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", blobtype.ErrDecompression, err)
		}
		return dec, closeFn, nil
	default:
		return nil, nil, fmt.Errorf("unknown compression algorithm: %d", entry.Compression)
	}
}

// workerCount determines the number of workers to use for processing.
func (p *Processor) workerCount(entries []*Entry) int {
	if len(entries) < 2 {
		return 1
	}
	if p.workers < 0 {
		return 1
	}

	workers := p.workers
	if workers == 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers < 2 {
			return 1
		}
		// Use size-based heuristic: only parallelize for larger entries
		var total uint64
		for _, entry := range entries {
			next, ok := sizing.AddUint64(total, entry.OriginalSize)
			if !ok {
				total = ^uint64(0)
				break
			}
			total = next
		}
		if total/uint64(len(entries)) < parallelMinAvgBytes {
			return 1
		}
	}

	if workers > len(entries) {
		workers = len(entries)
	}
	if workers < 2 {
		return 1
	}
	return workers
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
