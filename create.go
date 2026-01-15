package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/klauspost/compress/zstd"

	"github.com/meigma/blob/internal/fb"
	"github.com/meigma/blob/internal/platform"
	"github.com/meigma/blob/internal/write"
)

// DefaultMaxFiles is the default limit used when no MaxFiles option is set.
const DefaultMaxFiles = 200_000

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
func Create(ctx context.Context, dir string, indexW, dataW io.Writer, opts ...CreateOption) error {
	cfg := createConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	w := &writer{cfg: cfg}
	entries, err := w.writeData(ctx, root, dataW)
	if err != nil {
		return err
	}

	indexData := buildIndex(entries)
	_, err = indexW.Write(indexData)
	return err
}

// writer is the internal writer implementation.
type writer struct {
	cfg createConfig
}

// writeData streams file contents to the data writer while populating entries.
func (w *writer) writeData(ctx context.Context, root *os.Root, data io.Writer) ([]Entry, error) {
	entries := make([]Entry, 0, 1024)
	var offset uint64
	strict := w.cfg.changeDetection == ChangeDetectionStrict
	maxFiles := w.cfg.maxFiles
	if maxFiles == 0 {
		maxFiles = DefaultMaxFiles
	}

	var enc *zstd.Encoder
	if w.cfg.compression != CompressionNone {
		var err error
		enc, err = zstd.NewWriter(io.Discard, zstd.WithEncoderConcurrency(1), zstd.WithLowerEncoderMem(true))
		if err != nil {
			return nil, fmt.Errorf("create zstd encoder: %w", err)
		}
	}
	buf := make([]byte, 32*1024)

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
//
//nolint:gocritic // unnamedResult is acceptable for this internal helper
func (w *writer) processEntry(ctx context.Context, root *os.Root, data io.Writer, enc *zstd.Encoder, buf []byte, path string, d fs.DirEntry, walkErr error, strict bool, maxFiles, count int) (Entry, bool, error) {
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
	info, ok, err := write.ResolveEntryInfo(root, fsPath, d, strict)
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
		if errors.Is(err, platform.ErrSymlink) {
			return Entry{}, true, nil
		}
		return Entry{}, false, err
	}

	return entry, false, nil
}

func (w *writer) writeEntry(ctx context.Context, root *os.Root, data io.Writer, enc *zstd.Encoder, buf []byte, path, fsPath string, info fs.FileInfo, strict bool) (Entry, error) {
	f, err := platform.OpenFileNoFollow(root, fsPath)
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
	if validateErr := write.ValidateFileInfo(path, info, finfo, strict); validateErr != nil {
		return Entry{}, validateErr
	}

	compression := w.cfg.compression
	if compression != CompressionNone && write.ShouldSkip(path, finfo, w.cfg.skipCompression) {
		compression = CompressionNone
	}

	if finfo.Size() < 0 {
		return Entry{}, fmt.Errorf("negative file size: %s", path)
	}

	dataSize, originalSize, hash, err := write.File(ctx, f, data, enc, buf, compression, finfo.Size())
	if err != nil {
		return Entry{}, fmt.Errorf("write %s: %w", path, err)
	}

	if err := write.CheckFileUnchanged(f, path, finfo, strict); err != nil {
		return Entry{}, err
	}

	uid, gid := platform.FileOwner(finfo)
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

// buildIndex serializes entries to FlatBuffers format.
func buildIndex(entries []Entry) []byte {
	builder := flatbuffers.NewBuilder(1024)

	// Build entries in reverse order (FlatBuffers requirement)
	entryOffsets := make([]flatbuffers.UOffsetT, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]

		pathOffset := builder.CreateString(e.Path)

		fb.EntryStartHashVector(builder, len(e.Hash))
		for j := len(e.Hash) - 1; j >= 0; j-- {
			builder.PrependByte(e.Hash[j])
		}
		hashOffset := builder.EndVector(len(e.Hash))

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

	fb.IndexStartEntriesVector(builder, len(entries))
	for i := len(entryOffsets) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(entryOffsets[i])
	}
	entriesOffset := builder.EndVector(len(entries))

	fb.IndexStart(builder)
	fb.IndexAddVersion(builder, 1)
	fb.IndexAddHashAlgorithm(builder, fb.HashAlgorithmSHA256)
	fb.IndexAddEntries(builder, entriesOffset)
	indexOffset := fb.IndexEnd(builder)

	builder.Finish(indexOffset)
	return builder.FinishedBytes()
}
