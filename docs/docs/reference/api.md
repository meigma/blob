---
sidebar_position: 1
---

# API Reference

Complete reference for all public types, functions, and options in the blob library.

## Package blob

```
import "github.com/meigma/blob"
```

The blob package provides a file archive format optimized for random access via HTTP range requests against OCI registries.

### Types

#### Blob

```go
type Blob struct {
    // contains filtered or unexported fields
}
```

Blob provides random access to archive files. Blob implements `fs.FS`, `fs.StatFS`, `fs.ReadFileFS`, and `fs.ReadDirFS` for compatibility with the standard library.

#### BlobFile

```go
type BlobFile struct {
    *Blob
    // contains filtered or unexported fields
}
```

BlobFile wraps a `*Blob` with an underlying data file handle. It embeds `*Blob`, so all Blob methods are directly accessible. BlobFile must be closed to release file resources.

BlobFile implements `fs.FS`, `fs.StatFS`, `fs.ReadFileFS`, and `fs.ReadDirFS` for compatibility with the standard library.

#### Entry

```go
type Entry struct {
    Path         string
    DataOffset   uint64
    DataSize     uint64
    OriginalSize uint64
    Hash         []byte
    Mode         fs.FileMode
    UID          uint32
    GID          uint32
    ModTime      time.Time
    Compression  Compression
}
```

Entry represents a file in the archive.

| Field | Type | Description |
|-------|------|-------------|
| Path | `string` | File path relative to archive root (e.g., "src/main.go") |
| DataOffset | `uint64` | Byte offset in data blob where file content begins |
| DataSize | `uint64` | Size in bytes of file content in data blob (compressed size for compressed files) |
| OriginalSize | `uint64` | Uncompressed size in bytes (equals DataSize for uncompressed files) |
| Hash | `[]byte` | SHA256 hash of uncompressed file content |
| Mode | `fs.FileMode` | File permission bits |
| UID | `uint32` | File owner's user ID |
| GID | `uint32` | File owner's group ID |
| ModTime | `time.Time` | File modification time |
| Compression | `Compression` | Compression algorithm used for this file |

#### EntryView

```go
type EntryView struct {
    // contains filtered or unexported fields
}
```

EntryView provides a read-only view of an index entry. The byte slices returned by `PathBytes` and `HashBytes` alias the index buffer and must be treated as immutable. The view is only valid while the Blob that produced it remains alive.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| Path | `func (ev EntryView) Path() string` | Returns the path as a string |
| PathBytes | `func (ev EntryView) PathBytes() []byte` | Returns the path bytes from the index buffer |
| HashBytes | `func (ev EntryView) HashBytes() []byte` | Returns the SHA256 hash bytes from the index buffer |
| DataOffset | `func (ev EntryView) DataOffset() uint64` | Returns the data blob offset |
| DataSize | `func (ev EntryView) DataSize() uint64` | Returns the stored (possibly compressed) size |
| OriginalSize | `func (ev EntryView) OriginalSize() uint64` | Returns the uncompressed size |
| Mode | `func (ev EntryView) Mode() fs.FileMode` | Returns the file mode bits |
| UID | `func (ev EntryView) UID() uint32` | Returns the file owner's user ID |
| GID | `func (ev EntryView) GID() uint32` | Returns the file owner's group ID |
| ModTime | `func (ev EntryView) ModTime() time.Time` | Returns the modification time |
| Compression | `func (ev EntryView) Compression() Compression` | Returns the compression algorithm used |
| Entry | `func (ev EntryView) Entry() Entry` | Returns a fully copied Entry |

#### Compression

```go
type Compression uint8
```

Compression identifies the compression algorithm used for a file.

#### ByteSource

```go
type ByteSource interface {
    io.ReaderAt
    Size() int64
    SourceID() string
}
```

ByteSource provides random access to the data blob. Implementations exist for local files (`*os.File`) and HTTP range requests (`http.Source`).

| Method | Signature | Description |
|--------|-----------|-------------|
| ReadAt | `ReadAt(p []byte, off int64) (int, error)` | Reads bytes at the given offset (from io.ReaderAt). |
| Size | `Size() int64` | Returns the total size of the data blob. |
| SourceID | `SourceID() string` | Returns a stable identifier for the underlying content, used by block cache for cache keys. |

#### ChangeDetection

```go
type ChangeDetection uint8
```

ChangeDetection controls how strictly file changes are detected during archive creation.

#### SkipCompressionFunc

```go
type SkipCompressionFunc func(path string, info fs.FileInfo) bool
```

SkipCompressionFunc returns true when a file should be stored uncompressed. It is called once per file and should be inexpensive.

### Functions

#### New

```go
func New(indexData []byte, source ByteSource, opts ...Option) (*Blob, error)
```

New creates a Blob for accessing files in the archive.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| indexData | `[]byte` | FlatBuffers-encoded index blob |
| source | `ByteSource` | Provides access to file content |
| opts | `...Option` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| blob | `*Blob` | The created Blob accessor |
| err | `error` | Non-nil if index parsing fails |

#### OpenFile

```go
func OpenFile(indexPath, dataPath string, opts ...Option) (*BlobFile, error)
```

OpenFile opens a blob archive from local index and data files. The index file is read into memory; the data file is opened for random access. The returned BlobFile must be closed to release file resources.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| indexPath | `string` | Path to the index blob file |
| dataPath | `string` | Path to the data blob file |
| opts | `...Option` | Configuration options (same as New) |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| blobFile | `*BlobFile` | The opened archive with file handle |
| err | `error` | Non-nil if file opening or index parsing fails |

#### Create

```go
func Create(ctx context.Context, dir string, indexW, dataW io.Writer, opts ...CreateOption) error
```

Create builds an archive from the contents of a directory.

Files are written to the data writer in path-sorted order, enabling efficient directory fetches via single range requests. The index is written as a FlatBuffers-encoded blob to the index writer.

Create builds the entire index in memory; memory use scales with entry count and path length. Rough guide: ~30-50MB for 100k files with ~60B average paths.

Create walks dir recursively, including all regular files. Empty directories are not preserved. Symbolic links are not followed.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| dir | `string` | Source directory to archive |
| indexW | `io.Writer` | Destination for index blob |
| dataW | `io.Writer` | Destination for data blob |
| opts | `...CreateOption` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| err | `error` | Non-nil if archive creation fails |

#### CreateBlob

```go
func CreateBlob(ctx context.Context, srcDir, destDir string, opts ...CreateBlobOption) (*BlobFile, error)
```

CreateBlob builds an archive from a directory and returns an open BlobFile handle. The index and data files are written to destDir with default names (`index.blob` and `data.blob`). This is a convenience function that combines Create and OpenFile.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| srcDir | `string` | Source directory to archive |
| destDir | `string` | Destination directory for archive files |
| opts | `...CreateBlobOption` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| blobFile | `*BlobFile` | The created and opened archive |
| err | `error` | Non-nil if archive creation fails |

#### DefaultSkipCompression

```go
func DefaultSkipCompression(minSize int64) SkipCompressionFunc
```

DefaultSkipCompression returns a SkipCompressionFunc that skips small files and known already-compressed extensions.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| minSize | `int64` | Files smaller than this size are stored uncompressed |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| fn | `SkipCompressionFunc` | Predicate function for skip decisions |

**Skipped Extensions:**

`.7z`, `.aac`, `.avif`, `.br`, `.bz2`, `.flac`, `.gif`, `.gz`, `.heic`, `.ico`, `.jpeg`, `.jpg`, `.m4v`, `.mkv`, `.mov`, `.mp3`, `.mp4`, `.ogg`, `.opus`, `.pdf`, `.png`, `.rar`, `.tgz`, `.wav`, `.webm`, `.webp`, `.woff`, `.woff2`, `.xz`, `.zip`, `.zst`

### Blob Methods

#### Open

```go
func (b *Blob) Open(name string) (fs.File, error)
```

Open implements `fs.FS`. Returns an `fs.File` for reading the named file. The returned file verifies the content hash on Close (unless disabled by `WithVerifyOnClose`) and returns `ErrHashMismatch` if verification fails. Callers must read to EOF or Close to ensure integrity; partial reads may return unverified data.

#### Stat

```go
func (b *Blob) Stat(name string) (fs.FileInfo, error)
```

Stat implements `fs.StatFS`. Returns file info for the named file without reading its content. For directories (paths that are prefixes of other entries), Stat returns synthetic directory info.

#### ReadFile

```go
func (b *Blob) ReadFile(name string) ([]byte, error)
```

ReadFile implements `fs.ReadFileFS`. Reads and returns the entire contents of the named file. The content is decompressed if necessary and verified against its hash.

#### ReadDir

```go
func (b *Blob) ReadDir(name string) ([]fs.DirEntry, error)
```

ReadDir implements `fs.ReadDirFS`. Returns directory entries for the named directory, sorted by name. Directory entries are synthesized from file paths; the archive does not store directories explicitly.

#### CopyTo

```go
func (b *Blob) CopyTo(destDir string, paths ...string) error
```

CopyTo extracts specific files to a destination directory. Parent directories are created as needed.

**Default Behavior:**
- Existing files are skipped (use `CopyWithOverwrite` to overwrite)
- File modes and times are not preserved (use `CopyWithPreserveMode`/`CopyWithPreserveTimes`)
- Range reads are pipelined with concurrency 4 (use `CopyWithReadConcurrency` to change)

#### CopyToWithOptions

```go
func (b *Blob) CopyToWithOptions(destDir string, paths []string, opts ...CopyOption) error
```

CopyToWithOptions extracts specific files with options.

#### CopyDir

```go
func (b *Blob) CopyDir(destDir, prefix string, opts ...CopyOption) error
```

CopyDir extracts all files under a directory prefix to a destination. If prefix is "" or ".", all files in the archive are extracted.

Files are written atomically using temp files and renames by default. `CopyWithCleanDest` clears the destination prefix and writes directly to the final path. This is more performant but less safe.

**Default Behavior:**
- Existing files are skipped (use `CopyWithOverwrite` to overwrite)
- File modes and times are not preserved (use `CopyWithPreserveMode`/`CopyWithPreserveTimes`)
- Range reads are pipelined with concurrency 4 (use `CopyWithReadConcurrency` to change)

#### Entry

```go
func (b *Blob) Entry(path string) (EntryView, bool)
```

Entry returns a read-only view of the entry for the given path. The returned view is only valid while the Blob remains alive.

#### Entries

```go
func (b *Blob) Entries() iter.Seq[EntryView]
```

Entries returns an iterator over all entries as read-only views. The returned views are only valid while the Blob remains alive.

#### EntriesWithPrefix

```go
func (b *Blob) EntriesWithPrefix(prefix string) iter.Seq[EntryView]
```

EntriesWithPrefix returns an iterator over entries with the given prefix as read-only views. The returned views are only valid while the Blob remains alive.

#### Len

```go
func (b *Blob) Len() int
```

Len returns the number of entries in the archive.

#### Reader

```go
func (b *Blob) Reader() *file.Reader
```

Reader returns the underlying file reader. This is useful for cached readers that need to share the decompression pool.

#### IndexData

```go
func (b *Blob) IndexData() []byte
```

IndexData returns the raw FlatBuffers-encoded index data. This is useful for creating new Blobs with different data sources.

#### DataHash

```go
func (b *Blob) DataHash() ([]byte, bool)
```

DataHash returns the hash of the data blob bytes recorded in the index. The returned slice aliases the index buffer and must be treated as immutable. The boolean is false when the index does not record data metadata.

#### DataSize

```go
func (b *Blob) DataSize() (uint64, bool)
```

DataSize returns the data blob size in bytes recorded in the index. The boolean is false when the index does not record data metadata.

#### Stream

```go
func (b *Blob) Stream() io.Reader
```

Stream returns an `io.Reader` that streams the entire data blob from beginning to end. This is useful for copying or transmitting the complete data content.

#### Save

```go
func (b *Blob) Save(indexPath, dataPath string) error
```

Save writes the blob's index and data to the specified file paths. Uses atomic writes (temp file + rename) to prevent partial writes on failure. Parent directories are created as needed.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| indexPath | `string` | Path where index blob will be written |
| dataPath | `string` | Path where data blob will be written |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| err | `error` | Non-nil if file writing fails |

### BlobFile Methods

BlobFile embeds `*Blob`, so all Blob methods (Open, Stat, ReadFile, ReadDir, CopyTo, CopyDir, Entry, Entries, etc.) are directly accessible on BlobFile.

#### Close

```go
func (bf *BlobFile) Close() error
```

Close closes the underlying data file. Should be called to release file resources. Safe to call multiple times.

### Options

Options configure a Blob via the `New` function.

```go
type Option func(*Blob)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxFileSize(limit uint64)` | Per-file size limit (compressed and uncompressed). Set to 0 to disable. | 256MB |
| `WithMaxDecoderMemory(limit uint64)` | Zstd decoder memory limit. Set to 0 to disable. | 256MB |
| `WithDecoderConcurrency(n int)` | Zstd decoder thread count. Values &lt;0 use GOMAXPROCS. | 1 |
| `WithDecoderLowmem(enabled bool)` | Zstd decoder low-memory mode. | false |
| `WithVerifyOnClose(enabled bool)` | Hash verification on Close. When false, integrity is only guaranteed when callers read to EOF. | true |

### Copy Options

Copy options configure `CopyTo`, `CopyToWithOptions`, and `CopyDir` operations.

```go
type CopyOption func(*copyConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `CopyWithOverwrite(bool)` | Overwrite existing files. | false |
| `CopyWithPreserveMode(bool)` | Preserve file permission modes from the archive. | false |
| `CopyWithPreserveTimes(bool)` | Preserve file modification times from the archive. | false |
| `CopyWithCleanDest(bool)` | Clear destination prefix before copying, write directly (no temp files). Only supported by `CopyDir`. | false |
| `CopyWithWorkers(n int)` | Worker count for parallel processing. &lt;0 forces serial, 0 uses automatic heuristics, &gt;0 forces fixed count. | 0 (auto) |
| `CopyWithReadConcurrency(n int)` | Number of concurrent range reads. Use 1 for serial reads. | 4 |
| `CopyWithReadAheadBytes(limit uint64)` | Total size cap for buffered group data. 0 disables the byte budget. | 0 (unlimited) |

### Create Options

Create options configure archive creation via the `Create` function.

```go
type CreateOption func(*createConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `CreateWithCompression(c Compression)` | Compression algorithm. Use `CompressionNone` or `CompressionZstd`. | CompressionNone |
| `CreateWithChangeDetection(cd ChangeDetection)` | File change detection during archive creation. | ChangeDetectionNone |
| `CreateWithSkipCompression(fns ...SkipCompressionFunc)` | Predicates that decide to store files uncompressed. If any returns true, compression is skipped. | none |
| `CreateWithMaxFiles(n int)` | Maximum file count. 0 uses `DefaultMaxFiles`, &lt;0 means unlimited. | 200,000 |

### CreateBlob Options

CreateBlob options configure archive creation and file naming via the `CreateBlob` function.

```go
type CreateBlobOption func(*createBlobConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `CreateBlobWithIndexName(name string)` | Override the index filename. | "index.blob" |
| `CreateBlobWithDataName(name string)` | Override the data filename. | "data.blob" |
| `CreateBlobWithCompression(c Compression)` | Compression algorithm. Use `CompressionNone` or `CompressionZstd`. | CompressionNone |
| `CreateBlobWithChangeDetection(cd ChangeDetection)` | File change detection during archive creation. | ChangeDetectionNone |
| `CreateBlobWithSkipCompression(fns ...SkipCompressionFunc)` | Predicates that decide to store files uncompressed. | none |
| `CreateBlobWithMaxFiles(n int)` | Maximum file count. 0 uses `DefaultMaxFiles`, &lt;0 means unlimited. | 200,000 |

### Constants

#### Compression Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `CompressionNone` | 0 | No compression |
| `CompressionZstd` | 1 | Zstandard compression |

#### ChangeDetection Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `ChangeDetectionNone` | 0 | No change detection (default) |
| `ChangeDetectionStrict` | 1 | Verify files did not change during archive creation |

#### File Name Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultIndexName` | "index.blob" | Default filename for index when using CreateBlob |
| `DefaultDataName` | "data.blob" | Default filename for data when using CreateBlob |

#### Other Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultMaxFiles` | 200,000 | Default limit when no MaxFiles option is set |

### Errors

| Error | Description |
|-------|-------------|
| `ErrHashMismatch` | Content hash verification failed |
| `ErrDecompression` | Decompression failed |
| `ErrSizeOverflow` | Byte counts exceed supported limits |
| `ErrSymlink` | Symlink encountered where not allowed |
| `ErrTooManyFiles` | File count exceeded configured limit |

---

## Package blob/http

```
import "github.com/meigma/blob/http"
```

Package http provides a ByteSource backed by HTTP range requests.

### Types

#### Source

```go
type Source struct {
    // contains filtered or unexported fields
}
```

Source implements random access reads via HTTP range requests. It satisfies `blob.ByteSource` (`io.ReaderAt` plus `Size`).

### Functions

#### NewSource

```go
func NewSource(url string, opts ...Option) (*Source, error)
```

NewSource creates a Source backed by HTTP range requests. It probes the remote to determine the content size.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| url | `string` | URL of the remote resource |
| opts | `...Option` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| source | `*Source` | The created Source |
| err | `error` | Non-nil if metadata probe fails |

### Source Methods

#### Size

```go
func (s *Source) Size() int64
```

Size returns the total size of the remote content.

#### SourceID

```go
func (s *Source) SourceID() string
```

SourceID returns a stable identifier for the remote content. The identifier is derived from the URL and, when available, ETag or Last-Modified headers. Used as part of block cache keys.

#### ReadAt

```go
func (s *Source) ReadAt(p []byte, off int64) (int, error)
```

ReadAt reads data from the remote at the given offset using HTTP range requests. Implements `io.ReaderAt`.

#### ReadRange

```go
func (s *Source) ReadRange(off, length int64) (io.ReadCloser, error)
```

ReadRange returns a reader for the specified byte range.

### Options

```go
type Option func(*Source)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithClient(client *http.Client)` | HTTP client used for requests | `http.DefaultClient` |
| `WithHeaders(headers http.Header)` | Additional headers on each request | none |
| `WithHeader(key, value string)` | Single header on each request | none |
| `WithSourceID(id string)` | Override the automatic source identifier used for block cache keys | auto-generated |

---

## Package blob/cache

```
import "github.com/meigma/blob/cache"
```

Package cache provides content-addressed caching for blob archives.

### Interfaces

#### Cache

```go
type Cache interface {
    Get(hash []byte) (fs.File, bool)
    Put(hash []byte, f fs.File) error
    Delete(hash []byte) error
    MaxBytes() int64
    SizeBytes() int64
    Prune(targetBytes int64) (int64, error)
}
```

Cache provides content-addressed storage for file contents. Keys are SHA256 hashes of uncompressed file content. Values are the uncompressed content. Because keys are content hashes, cache hits are implicitly verified.

Implementations must be safe for concurrent use.

| Method | Signature | Description |
|--------|-----------|-------------|
| Get | `Get(hash []byte) (fs.File, bool)` | Retrieves content by its SHA256 hash. Returns `nil, false` if not cached. |
| Put | `Put(hash []byte, f fs.File) error` | Stores content by reading from the provided fs.File. |
| Delete | `Delete(hash []byte) error` | Removes cached content for the given hash. |
| MaxBytes | `MaxBytes() int64` | Returns the configured cache size limit (0 = unlimited). |
| SizeBytes | `SizeBytes() int64` | Returns the current cache size in bytes. |
| Prune | `Prune(targetBytes int64) (int64, error)` | Removes entries until cache is at or below targetBytes. Returns bytes freed. |

#### StreamingCache

```go
type StreamingCache interface {
    Cache
    Writer(hash []byte) (Writer, error)
}
```

StreamingCache extends Cache with streaming write support for large files. Implementations that support streaming (e.g., disk-based caches) should implement this interface to allow caching during `Open()` without buffering entire files in memory.

| Method | Signature | Description |
|--------|-----------|-------------|
| Writer | `Writer(hash []byte) (Writer, error)` | Returns a Writer for streaming content into the cache. The hash is the expected SHA256 of the content being written. |

#### Writer

```go
type Writer interface {
    io.Writer
    Commit() error
    Discard() error
}
```

Writer streams content into the cache. Content is written via Write calls. After all content is written, call `Commit` if verification succeeded or `Discard` if verification failed.

| Method | Signature | Description |
|--------|-----------|-------------|
| Write | `Write(p []byte) (n int, err error)` | Writes content to the cache buffer. |
| Commit | `Commit() error` | Finalizes the cache entry, making it available via Get. |
| Discard | `Discard() error` | Aborts the cache write and cleans up temporary data. |

#### ByteSource

```go
type ByteSource interface {
    io.ReaderAt
    Size() int64
    SourceID() string
}
```

ByteSource provides random access to data for block caching. This mirrors the `blob.ByteSource` interface.

| Method | Signature | Description |
|--------|-----------|-------------|
| ReadAt | `ReadAt(p []byte, off int64) (int, error)` | Reads bytes at the given offset (from io.ReaderAt). |
| Size | `Size() int64` | Returns the total size of the source. |
| SourceID | `SourceID() string` | Returns a stable identifier for cache key generation. |

#### BlockCache

```go
type BlockCache interface {
    Wrap(src ByteSource, opts ...WrapOption) (ByteSource, error)
    MaxBytes() int64
    SizeBytes() int64
    Prune(targetBytes int64) (int64, error)
}
```

BlockCache wraps ByteSources with block-level caching. Block caching is most effective for random, non-contiguous reads over remote sources.

| Method | Signature | Description |
|--------|-----------|-------------|
| Wrap | `Wrap(src ByteSource, opts ...WrapOption) (ByteSource, error)` | Wraps a source with block caching. |
| MaxBytes | `MaxBytes() int64` | Returns the configured cache size limit (0 = unlimited). |
| SizeBytes | `SizeBytes() int64` | Returns the current cache size in bytes. |
| Prune | `Prune(targetBytes int64) (int64, error)` | Removes entries until cache is at or below targetBytes. |

### Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultBlockSize` | 65536 (64 KB) | Default block size for block caches |
| `DefaultMaxBlocksPerRead` | 4 | Default threshold for bypassing cache on large reads |

### Wrap Options

```go
type WrapOption func(*WrapConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithBlockSize(n int64)` | Block size in bytes for caching. | 64 KB |
| `WithMaxBlocksPerRead(n int)` | Bypass caching when read spans more than n blocks. 0 disables the limit. | 4 |

### Types

#### Blob

```go
type Blob struct {
    // contains filtered or unexported fields
}
```

Blob wraps a `blob.Blob` with content-addressed caching. Blob implements `fs.FS`, `fs.StatFS`, `fs.ReadFileFS`, and `fs.ReadDirFS`.

For streaming reads via `Open()`, caching behavior depends on the cache type:
- `StreamingCache`: content streams to cache without full buffering
- Basic `Cache`: content is buffered in memory then cached on Close

Blob uses singleflight to deduplicate concurrent `ReadFile` calls for the same content, preventing redundant network requests during cache miss storms.

### Functions

#### New

```go
func New(base *blob.Blob, cache Cache, opts ...Option) *Blob
```

New wraps a `blob.Blob` with caching support.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| base | `*blob.Blob` | The underlying Blob to wrap |
| cache | `Cache` | Cache implementation to use |
| opts | `...Option` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| blob | `*Blob` | The cached Blob wrapper |

### Blob Methods

#### Open

```go
func (b *Blob) Open(name string) (fs.File, error)
```

Open implements `fs.FS` with caching support. For files, the returned `fs.File` will cache content after successful hash verification on Close. For `StreamingCache` implementations, content streams directly to the cache. For basic `Cache` implementations, content is buffered in memory.

#### Stat

```go
func (b *Blob) Stat(name string) (fs.FileInfo, error)
```

Stat implements `fs.StatFS`. Delegates to the underlying Blob.

#### ReadFile

```go
func (b *Blob) ReadFile(name string) ([]byte, error)
```

ReadFile implements `fs.ReadFileFS` with caching support. Checks the cache first and returns cached content if available. On cache miss, reads from the source and caches the result. Concurrent calls for the same content are deduplicated using singleflight.

#### ReadDir

```go
func (b *Blob) ReadDir(name string) ([]fs.DirEntry, error)
```

ReadDir implements `fs.ReadDirFS`. Delegates to the underlying Blob.

#### Prefetch

```go
func (b *Blob) Prefetch(paths ...string) error
```

Prefetch fetches and caches the specified files. For adjacent files, Prefetch batches range requests to minimize round trips. This is useful for warming the cache with files that will be accessed soon.

#### PrefetchDir

```go
func (b *Blob) PrefetchDir(prefix string) error
```

PrefetchDir fetches and caches all files under the given directory prefix. Because files are sorted by path and stored adjacently, PrefetchDir can fetch an entire directory's contents with a single range request, then split and cache each file individually.

### Options

```go
type Option func(*Blob)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithPrefetchConcurrency(workers int)` | Number of workers for Prefetch/PrefetchDir. &lt;0 forces serial, 0 uses default (serial), &gt;0 forces fixed count. | 0 (serial) |

---

## Package blob/cache/disk

```
import "github.com/meigma/blob/cache/disk"
```

Package disk provides a disk-backed cache implementation.

### Types

#### Cache

```go
type Cache struct {
    // contains filtered or unexported fields
}
```

Cache implements `cache.Cache` and `cache.StreamingCache` using the local filesystem.

### Functions

#### New

```go
func New(dir string, opts ...Option) (*Cache, error)
```

New creates a disk-backed cache rooted at the specified directory.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| dir | `string` | Root directory for the cache |
| opts | `...Option` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| cache | `*Cache` | The created disk cache |
| err | `error` | Non-nil if directory creation fails |

### Cache Methods

#### Get

```go
func (c *Cache) Get(hash []byte) ([]byte, bool)
```

Get retrieves content by its SHA256 hash. Returns `nil, false` if the content is not cached.

#### Put

```go
func (c *Cache) Put(hash, content []byte) error
```

Put stores content indexed by its SHA256 hash. Writes are atomic using temp files and renames.

#### Writer

```go
func (c *Cache) Writer(hash []byte) (cache.Writer, error)
```

Writer opens a streaming cache writer for the given hash. Implements `cache.StreamingCache`.

### Options

```go
type Option func(*Cache)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithShardPrefixLen(n int)` | Number of hex characters used for directory sharding. Use 0 to disable sharding. | 2 |
| `WithDirPerm(mode os.FileMode)` | Directory permissions for cache directories. | 0700 |
| `WithMaxBytes(n int64)` | Maximum cache size in bytes. 0 disables the limit. | 0 (unlimited) |

### Cache Methods

#### MaxBytes

```go
func (c *Cache) MaxBytes() int64
```

MaxBytes returns the configured cache size limit (0 = unlimited).

#### SizeBytes

```go
func (c *Cache) SizeBytes() int64
```

SizeBytes returns the current cache size in bytes.

#### Prune

```go
func (c *Cache) Prune(targetBytes int64) (int64, error)
```

Prune removes cached entries until the cache is at or below targetBytes. Returns the number of bytes freed.

---

### BlockCache Type

```go
type BlockCache struct {
    // contains filtered or unexported fields
}
```

BlockCache provides a disk-backed block cache for ByteSources. It implements `cache.BlockCache`.

### BlockCache Functions

#### NewBlockCache

```go
func NewBlockCache(dir string, opts ...BlockCacheOption) (*BlockCache, error)
```

NewBlockCache creates a disk-backed block cache rooted at the specified directory.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| dir | `string` | Root directory for the block cache |
| opts | `...BlockCacheOption` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| cache | `*BlockCache` | The created block cache |
| err | `error` | Non-nil if directory creation fails |

### BlockCache Methods

#### Wrap

```go
func (c *BlockCache) Wrap(src cache.ByteSource, opts ...cache.WrapOption) (cache.ByteSource, error)
```

Wrap returns a ByteSource that caches reads in fixed-size blocks.

#### MaxBytes

```go
func (c *BlockCache) MaxBytes() int64
```

MaxBytes returns the configured cache size limit (0 = unlimited).

#### SizeBytes

```go
func (c *BlockCache) SizeBytes() int64
```

SizeBytes returns the current cache size in bytes.

#### Prune

```go
func (c *BlockCache) Prune(targetBytes int64) (int64, error)
```

Prune removes cached entries until the cache is at or below targetBytes.

### BlockCache Options

```go
type BlockCacheOption func(*BlockCache)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithBlockMaxBytes(n int64)` | Maximum cache size in bytes. 0 disables the limit. | 0 (unlimited) |
| `WithBlockShardPrefixLen(n int)` | Number of hex characters for directory sharding. 0 disables sharding. | 2 |
| `WithBlockDirPerm(mode os.FileMode)` | Directory permissions for cache directories. | 0700 |
