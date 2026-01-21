package batch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/meigma/blob/core/internal/blobtype"
	"github.com/meigma/blob/core/internal/file"
	"github.com/meigma/blob/core/internal/sizing"
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
	source           file.ByteSource
	pool             *file.DecompressPool
	maxFileSize      uint64
	workers          int // 0 = auto, <0 = serial, >0 = fixed count
	readConcurrency  int
	readAheadBytes   uint64
	readAheadEnabled bool
	logger           *slog.Logger
}

// log returns the logger, falling back to a discard logger if nil.
func (p *Processor) log() *slog.Logger {
	if p.logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return p.logger
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

// WithReadConcurrency sets the number of concurrent range reads.
// Values < 1 force serial reads.
func WithReadConcurrency(n int) ProcessorOption {
	return func(p *Processor) {
		if n < 1 {
			n = 1
		}
		p.readConcurrency = n
	}
}

// WithReadAheadBytes caps the total size of buffered group data.
// A value of 0 disables the byte budget.
func WithReadAheadBytes(limit uint64) ProcessorOption {
	return func(p *Processor) {
		p.readAheadBytes = limit
		p.readAheadEnabled = limit > 0
	}
}

// WithProcessorLogger sets the logger for batch processing operations.
// If not set, logging is disabled.
func WithProcessorLogger(logger *slog.Logger) ProcessorOption {
	return func(p *Processor) {
		p.logger = logger
	}
}

// NewProcessor creates a new batch processor.
//
// The source provides random access to the data blob.
// The pool provides reusable zstd decoders for compressed entries.
// maxFileSize limits the size of individual entries (0 for no limit).
func NewProcessor(source file.ByteSource, pool *file.DecompressPool, maxFileSize uint64, opts ...ProcessorOption) *Processor {
	p := &Processor{
		source:          source,
		pool:            pool,
		maxFileSize:     maxFileSize,
		readConcurrency: 1,
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
		if err := file.ValidateAll(entry, sourceSize, p.maxFileSize); err != nil {
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
	p.log().Debug("batch processing", "entries", len(toProcess), "groups", len(groups))

	if len(groups) > 1 && (p.readConcurrency > 1 || p.readAheadEnabled) {
		return p.processGroupsPipelined(groups, sink)
	}
	return p.processGroupsSequential(groups, sink)
}

// groupTask represents a pending group read operation for the pipeline.
type groupTask struct {
	index int
	group rangeGroup
	size  int64
}

// groupResult holds the completed read data for a group.
type groupResult struct {
	index int
	group rangeGroup
	data  []byte
	size  int64
}

// processGroupsSequential processes groups one at a time without pipelining.
func (p *Processor) processGroupsSequential(groups []rangeGroup, sink Sink) error {
	for _, group := range groups {
		if err := p.processGroup(group, sink); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocognit,gocyclo // complex pipeline logic requires coordination between producers/consumers
func (p *Processor) processGroupsPipelined(groups []rangeGroup, sink Sink) error {
	if len(groups) == 0 {
		return nil
	}

	readWorkers := p.readConcurrency
	if readWorkers < 1 {
		readWorkers = 1
	}

	var budget *semaphore.Weighted
	if p.readAheadEnabled {
		limit, err := sizing.ToInt64(p.readAheadBytes, blobtype.ErrSizeOverflow)
		if err != nil {
			return fmt.Errorf("batch: %w", err)
		}
		budget = semaphore.NewWeighted(limit)
	}

	readCh := make(chan groupTask)
	readyCh := make(chan groupResult, readWorkers)
	eg, ctx := errgroup.WithContext(context.Background())

	var readWg sync.WaitGroup
	readWg.Add(readWorkers)

	for range readWorkers {
		eg.Go(func() error {
			defer readWg.Done()
			for task := range readCh {
				if err := ctx.Err(); err != nil {
					return err
				}
				if budget != nil {
					if err := budget.Acquire(ctx, task.size); err != nil {
						return err
					}
				}
				data, err := p.readGroupData(task.group)
				if err != nil {
					if budget != nil {
						budget.Release(task.size)
					}
					return err
				}
				result := groupResult{
					index: task.index,
					group: task.group,
					data:  data,
					size:  task.size,
				}
				select {
				case readyCh <- result:
				case <-ctx.Done():
					if budget != nil {
						budget.Release(task.size)
					}
					return ctx.Err()
				}
			}
			return nil
		})
	}

	eg.Go(func() error {
		defer close(readCh)
		for i, group := range groups {
			size, err := groupSize(group)
			if err != nil {
				return err
			}
			task := groupTask{index: i, group: group, size: size}
			select {
			case readCh <- task:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	go func() {
		readWg.Wait()
		close(readyCh)
	}()

	eg.Go(func() error {
		next := 0
		pending := make(map[int]groupResult, readWorkers)
		for next < len(groups) {
			select {
			case res, ok := <-readyCh:
				if !ok {
					if err := ctx.Err(); err != nil {
						return err
					}
					return errors.New("batch: read pipeline ended unexpectedly")
				}
				pending[res.index] = res
				for {
					res, ok := pending[next]
					if !ok {
						break
					}
					delete(pending, next)
					if err := p.processGroupWithData(res.group, res.data, sink); err != nil {
						if budget != nil {
							budget.Release(res.size)
						}
						return err
					}
					if budget != nil {
						budget.Release(res.size)
					}
					next++
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	return eg.Wait()
}

// processGroup reads a contiguous range and processes each entry.
func (p *Processor) processGroup(group rangeGroup, sink Sink) error {
	data, err := p.readGroupData(group)
	if err != nil {
		return err
	}
	return p.processGroupWithData(group, data, sink)
}

// processGroupWithData processes all entries in a group using pre-fetched data.
func (p *Processor) processGroupWithData(group rangeGroup, data []byte, sink Sink) error {
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

// groupSize returns the total byte size of a group as int64.
func groupSize(group rangeGroup) (int64, error) {
	size := group.end - group.start
	return sizing.ToInt64(size, blobtype.ErrSizeOverflow)
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

// writeVerifyUncompressed verifies uncompressed data and writes it to w.
func (p *Processor) writeVerifyUncompressed(entry *Entry, data []byte, w io.Writer) error {
	if err := p.verifyUncompressed(entry, data); err != nil {
		return err
	}
	return writeAll(w, data)
}

// verifyUncompressed checks size and hash for uncompressed data.
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

// decompress decompresses entry data based on its compression type.
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
		if err := file.EnsureNoExtra(dec); err != nil {
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
	if err := file.EnsureNoExtra(tee); err != nil {
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

// writeAll writes all data to w, handling partial writes.
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
