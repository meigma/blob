package blob

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/klauspost/compress/zstd"

	"github.com/meigma/blob/internal/fb"
	"github.com/meigma/blob/internal/ioutil"
)

// DefaultMaxFiles is the default limit used when WriteOptions.MaxFiles is zero.
const DefaultMaxFiles = 200_000

// ChangeDetection controls how strictly file changes are detected during creation.
type ChangeDetection uint8

const (
	ChangeDetectionNone ChangeDetection = iota
	ChangeDetectionStrict
)

// SkipCompressionFunc returns true when a file should be stored uncompressed.
// It is called once per file and should be inexpensive.
type SkipCompressionFunc func(path string, info fs.FileInfo) bool

// WriteOptions configures archive creation.
type WriteOptions struct {
	// Compression specifies the algorithm to use for compressing files.
	// Use CompressionNone to store files uncompressed.
	Compression Compression

	// ChangeDetection controls whether the writer verifies files did not change
	// during archive creation. The zero value disables change detection to reduce
	// syscalls; enable ChangeDetectionStrict for stronger guarantees.
	ChangeDetection ChangeDetection

	// SkipCompression contains predicates that decide to store a file uncompressed.
	// If any predicate returns true, compression is skipped for that file.
	// These checks are on the hot path, so keep them cheap.
	SkipCompression []SkipCompressionFunc

	// MaxFiles limits the number of files included in the archive.
	// Zero uses DefaultMaxFiles. Negative means no limit.
	MaxFiles int
}

// DefaultSkipCompression returns a SkipCompressionFunc that skips small files
// and known already-compressed extensions.
func DefaultSkipCompression(minSize int64) SkipCompressionFunc {
	return func(path string, info fs.FileInfo) bool {
		if info != nil && minSize > 0 && info.Size() < minSize {
			return true
		}
		ext := strings.ToLower(filepath.Ext(path))
		_, ok := defaultSkipCompressionExts[ext]
		return ok
	}
}

// Writer creates archives from directories.
type Writer struct {
	opts WriteOptions
}

// NewWriter creates a Writer with the given options.
func NewWriter(opts WriteOptions) *Writer {
	return &Writer{opts: opts}
}

// Create builds an archive from the contents of dir.
//
// Files are written to the data writer in path-sorted order, enabling
// efficient directory fetches via single range requests. The index is
// written as a FlatBuffers-encoded blob to the index writer.
//
// Create builds the entire index in memory; memory use scales with entry
// count and path length. Rough guide: ~30-50MB for 100k files with ~60B
// average paths (entries plus FlatBuffers buffer).
//
// Create walks dir recursively, including all regular files. Empty
// directories are not preserved. Symbolic links are not followed.
//
// The context can be used for cancellation of long-running archive creation.
func (w *Writer) Create(ctx context.Context, dir string, index, data io.Writer) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	// Write file contents to data blob while collecting metadata
	entries, err := w.writeData(ctx, root, data)
	if err != nil {
		return err
	}

	// Build and write index
	indexData := buildIndex(entries)
	_, err = index.Write(indexData)
	return err
}

func (w *Writer) shouldSkipCompression(path string, info fs.FileInfo) bool {
	for _, fn := range w.opts.SkipCompression {
		if fn == nil {
			continue
		}
		if fn(path, info) {
			return true
		}
	}
	return false
}

// writeData streams file contents to the data writer while populating entries.
func (w *Writer) writeData(ctx context.Context, root *os.Root, data io.Writer) ([]Entry, error) {
	entries := make([]Entry, 0, 1024)
	var offset uint64
	buf := make([]byte, 32*1024)
	strict := w.opts.ChangeDetection == ChangeDetectionStrict
	maxFiles := w.opts.MaxFiles
	if maxFiles == 0 {
		maxFiles = DefaultMaxFiles
	}

	var enc *zstd.Encoder
	if w.opts.Compression != CompressionNone {
		var err error
		enc, err = zstd.NewWriter(io.Discard, zstd.WithEncoderConcurrency(1), zstd.WithLowerEncoderMem(true))
		if err != nil {
			return nil, fmt.Errorf("create zstd encoder: %w", err)
		}
	}

	err := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		entry, skip, err := w.processEntry(ctx, root, data, enc, buf, path, d, walkErr, strict, maxFiles, len(entries))
		if err != nil || skip {
			return err
		}
		if entry.DataSize > ^uint64(0)-offset {
			return ErrSizeOverflow
		}
		entry.DataOffset = offset
		entries = append(entries, entry)
		offset += entry.DataSize
		return nil
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// processEntry handles a single directory entry during archive creation.
// Returns (entry, skip, error) where skip indicates this entry should be skipped.
func (w *Writer) processEntry(ctx context.Context, root *os.Root, data io.Writer, enc *zstd.Encoder, buf []byte, path string, d fs.DirEntry, walkErr error, strict bool, maxFiles, count int) (Entry, bool, error) {
	if walkErr != nil {
		return Entry{}, false, walkErr
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, false, err
	}
	if d.IsDir() {
		return Entry{}, true, nil
	}

	fsPath := filepath.FromSlash(path)
	info, ok, err := entryInfo(root, fsPath, d, strict)
	if err != nil {
		return Entry{}, false, err
	}
	if !ok {
		return Entry{}, true, nil
	}

	if maxFiles > 0 && count >= maxFiles {
		return Entry{}, false, ErrTooManyFiles
	}

	entry, err := w.writeEntry(ctx, root, data, enc, buf, path, fsPath, info, strict)
	if err != nil {
		if errors.Is(err, ErrSymlink) {
			return Entry{}, true, nil
		}
		return Entry{}, false, err
	}

	return entry, false, nil
}

func (w *Writer) writeEntry(ctx context.Context, root *os.Root, data io.Writer, enc *zstd.Encoder, buf []byte, path, fsPath string, info fs.FileInfo, strict bool) (Entry, error) {
	f, err := openFileNoFollow(root, fsPath)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()

	finfo, err := f.Stat()
	if err != nil {
		return Entry{}, err
	}
	if !finfo.Mode().IsRegular() {
		return Entry{}, fmt.Errorf("not a regular file: %s", path)
	}
	if verr := validateFileInfo(path, info, finfo, strict); verr != nil {
		return Entry{}, verr
	}

	compression := w.opts.Compression
	if compression != CompressionNone && w.shouldSkipCompression(path, finfo) {
		compression = CompressionNone
	}

	if finfo.Size() < 0 {
		return Entry{}, fmt.Errorf("negative file size: %s", path)
	}

	dataSize, originalSize, hash, err := w.writeFile(ctx, f, data, enc, buf, compression, finfo.Size())
	if err != nil {
		return Entry{}, fmt.Errorf("write %s: %w", path, err)
	}

	if err := checkFileUnchanged(f, path, finfo, strict); err != nil {
		return Entry{}, err
	}

	uid, gid := fileOwner(finfo)
	return Entry{
		Path:         path,
		DataSize:     dataSize,
		OriginalSize: originalSize,
		Hash:         hash,
		Mode:         finfo.Mode().Perm(),
		UID:          uid,
		GID:          gid,
		ModTime:      finfo.ModTime(),
		Compression:  compression,
	}, nil
}

// writeFile streams a single file through the hash and optional compression
// pipeline to the data writer.
func (w *Writer) writeFile(ctx context.Context, f *os.File, data io.Writer, enc *zstd.Encoder, buf []byte, compression Compression, expectedSize int64) (dataSize, originalSize uint64, hash []byte, err error) {
	if expectedSize < 0 {
		return 0, 0, nil, errors.New("negative file size")
	}

	hasher := sha256.New()
	cw := &ioutil.CountingWriter{W: data}
	cr := &ioutil.CountingReader{R: io.LimitReader(f, expectedSize)}

	if compression == CompressionNone {
		// Stream: file → TeeReader(hasher) → countingWriter(data)
		if _, err := ioutil.CopyWithContext(ctx, cw, io.TeeReader(cr, hasher), buf); err != nil {
			return 0, 0, nil, wrapOverflowErr(err)
		}
	} else {
		// Stream: file → TeeReader(hasher) → zstd encoder → countingWriter(data)
		enc.Reset(cw)
		if _, err := ioutil.CopyWithContext(ctx, enc, io.TeeReader(cr, hasher), buf); err != nil {
			enc.Close()
			return 0, 0, nil, wrapOverflowErr(err)
		}
		if err := enc.Close(); err != nil {
			return 0, 0, nil, fmt.Errorf("close zstd encoder: %w", err)
		}
	}

	if cr.N != uint64(expectedSize) {
		return 0, 0, nil, fmt.Errorf("file size changed during archive creation: expected %d, got %d", expectedSize, cr.N)
	}

	return cw.N, cr.N, hasher.Sum(nil), nil
}

// buildIndex serializes entries to FlatBuffers format.
func buildIndex(entries []Entry) []byte {
	builder := flatbuffers.NewBuilder(1024)

	// Build entries in reverse order (FlatBuffers requirement)
	entryOffsets := make([]flatbuffers.UOffsetT, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]

		// Create path string
		pathOffset := builder.CreateString(e.Path)

		// Create hash vector (bytes in reverse)
		fb.EntryStartHashVector(builder, len(e.Hash))
		for j := len(e.Hash) - 1; j >= 0; j-- {
			builder.PrependByte(e.Hash[j])
		}
		hashOffset := builder.EndVector(len(e.Hash))

		// Build entry
		fb.EntryStart(builder)
		fb.EntryAddPath(builder, pathOffset)
		fb.EntryAddDataOffset(builder, e.DataOffset)
		fb.EntryAddDataSize(builder, e.DataSize)
		fb.EntryAddOriginalSize(builder, e.OriginalSize)
		fb.EntryAddHash(builder, hashOffset)
		fb.EntryAddMode(builder, uint32(e.Mode))
		fb.EntryAddUid(builder, e.UID)
		fb.EntryAddGid(builder, e.GID)
		fb.EntryAddMtimeNs(builder, e.ModTime.UnixNano())
		fb.EntryAddCompression(builder, fb.Compression(e.Compression)) //nolint:gosec // Compression is bounded 0-1
		entryOffsets[i] = fb.EntryEnd(builder)
	}

	// Create entries vector (must be in sorted order for binary search)
	fb.IndexStartEntriesVector(builder, len(entries))
	for i := len(entryOffsets) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(entryOffsets[i])
	}
	entriesOffset := builder.EndVector(len(entries))

	// Build index
	fb.IndexStart(builder)
	fb.IndexAddVersion(builder, 1)
	fb.IndexAddHashAlgorithm(builder, fb.HashAlgorithmSHA256)
	fb.IndexAddEntries(builder, entriesOffset)
	indexOffset := fb.IndexEnd(builder)

	builder.Finish(indexOffset)
	return builder.FinishedBytes()
}

func checkFileUnchanged(f *os.File, path string, before fs.FileInfo, strict bool) error {
	if !strict {
		return nil
	}
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode().Perm() != before.Mode().Perm() {
		return fmt.Errorf("file changed during archive creation: %s", path)
	}
	return nil
}

func entryInfo(root *os.Root, fsPath string, d fs.DirEntry, strict bool) (fs.FileInfo, bool, error) {
	dtype := d.Type()
	if dtype&fs.ModeSymlink != 0 {
		return nil, false, nil
	}

	if dtype == 0 {
		linfo, err := root.Lstat(fsPath)
		if err != nil {
			return nil, false, err
		}
		if linfo.Mode()&fs.ModeSymlink != 0 {
			return nil, false, nil
		}
		if !linfo.Mode().IsRegular() {
			return nil, false, nil
		}
		return linfo, true, nil
	}

	if !dtype.IsRegular() {
		return nil, false, nil
	}
	if !strict {
		return nil, true, nil
	}

	info, err := d.Info()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	return info, true, nil
}

func validateFileInfo(path string, info, finfo fs.FileInfo, strict bool) error {
	if !strict {
		return nil
	}
	if info == nil {
		return fmt.Errorf("missing file info: %s", path)
	}
	if !os.SameFile(info, finfo) {
		return fmt.Errorf("file changed during archive creation: %s", path)
	}
	return nil
}

func wrapOverflowErr(err error) error {
	if errors.Is(err, ioutil.ErrOverflow) {
		return ErrSizeOverflow
	}
	return err
}

var defaultSkipCompressionExts = map[string]struct{}{
	".7z":    {},
	".aac":   {},
	".avif":  {},
	".br":    {},
	".bz2":   {},
	".flac":  {},
	".gif":   {},
	".gz":    {},
	".heic":  {},
	".ico":   {},
	".jpeg":  {},
	".jpg":   {},
	".m4v":   {},
	".mkv":   {},
	".mov":   {},
	".mp3":   {},
	".mp4":   {},
	".ogg":   {},
	".opus":  {},
	".pdf":   {},
	".png":   {},
	".rar":   {},
	".tgz":   {},
	".wav":   {},
	".webm":  {},
	".webp":  {},
	".woff":  {},
	".woff2": {},
	".xz":    {},
	".zip":   {},
	".zst":   {},
}
