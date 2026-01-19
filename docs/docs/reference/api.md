---
sidebar_position: 1
---

# API Reference

Complete reference for the blob library. The primary API is `github.com/meigma/blob`, which provides everything most users need. Internal packages are documented at the end for advanced use cases.

## Package blob (Primary API)

```
import "github.com/meigma/blob"
```

The blob package provides a high-level API for pushing and pulling file archives to/from OCI registries.

---

### Client

```go
type Client struct {
    // contains filtered or unexported fields
}
```

Client provides operations for pushing and pulling blob archives to/from OCI registries.

#### NewClient

```go
func NewClient(opts ...Option) (*Client, error)
```

NewClient creates a new blob archive client with the given options.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| opts | `...Option` | Configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| client | `*Client` | The created client |
| err | `error` | Non-nil if option application fails |

---

### Client Methods

#### Push

```go
func (c *Client) Push(ctx context.Context, ref, srcDir string, opts ...PushOption) error
```

Push creates an archive from srcDir and pushes it to the registry. This is the primary workflow for pushing archives.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| ref | `string` | OCI reference with tag (e.g., "ghcr.io/org/repo:v1") |
| srcDir | `string` | Source directory to archive |
| opts | `...PushOption` | Push configuration options |

#### PushArchive

```go
func (c *Client) PushArchive(ctx context.Context, ref string, archive *blobcore.Blob, opts ...PushOption) error
```

PushArchive pushes an existing archive to the registry. Use when you have a pre-created archive from `blobcore.CreateBlob`.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| ref | `string` | OCI reference with tag |
| archive | `*blobcore.Blob` | Pre-created archive (from core package) |
| opts | `...PushOption` | Push configuration options |

#### Pull

```go
func (c *Client) Pull(ctx context.Context, ref string, opts ...PullOption) (*Archive, error)
```

Pull retrieves an archive from the registry with lazy data loading. File data is fetched on demand via HTTP range requests.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| ref | `string` | OCI reference (e.g., "ghcr.io/org/repo:v1") |
| opts | `...PullOption` | Pull configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| archive | `*Archive` | The pulled archive with lazy data loading |
| err | `error` | Non-nil if pull fails |

#### Fetch

```go
func (c *Client) Fetch(ctx context.Context, ref string, opts ...FetchOption) (*Manifest, error)
```

Fetch retrieves manifest metadata without downloading data. Use to check if an archive exists or inspect its metadata.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| ref | `string` | OCI reference |
| opts | `...FetchOption` | Fetch configuration options |

**Returns:**

| Return | Type | Description |
|--------|------|-------------|
| manifest | `*Manifest` | Manifest metadata |
| err | `error` | Non-nil if fetch fails |

#### Tag

```go
func (c *Client) Tag(ctx context.Context, ref, digest string) error
```

Tag creates or updates a tag pointing to an existing manifest.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| ctx | `context.Context` | Context for cancellation |
| ref | `string` | OCI reference with new tag |
| digest | `string` | Digest of existing manifest |

---

### Archive

```go
type Archive struct {
    *blobcore.Blob
}
```

Archive wraps a pulled blob archive with integrated caching. It embeds `*core.Blob`, so all Blob methods are directly accessible (Open, Stat, ReadFile, ReadDir, CopyTo, CopyDir, Entry, Entries, etc.).

Archive implements `fs.FS`, `fs.StatFS`, `fs.ReadFileFS`, and `fs.ReadDirFS` for compatibility with the standard library.

See [Blob Methods](#blob-methods) for the complete method list.

---

### Manifest

```go
type Manifest = registry.BlobManifest
```

Manifest represents a blob archive manifest from an OCI registry. This is an alias for `registry.BlobManifest`.

---

### Client Options

```go
type Option func(*Client) error
```

#### Authentication Options

| Option | Description |
|--------|-------------|
| `WithDockerConfig()` | Read credentials from ~/.docker/config.json (recommended) |
| `WithStaticCredentials(registry, username, password string)` | Set static username/password for a registry |
| `WithStaticToken(registry, token string)` | Set static bearer token for a registry |
| `WithAnonymous()` | Force anonymous access, ignoring any configured credentials |

#### Transport Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithPlainHTTP(bool)` | Use plain HTTP instead of HTTPS | false |
| `WithUserAgent(ua string)` | Set User-Agent header for registry requests | none |

#### Caching Options (Simple)

| Option | Description |
|--------|-------------|
| `WithCacheDir(dir string)` | Enable all caches with default sizes in subdirectories of dir |
| `WithContentCacheDir(dir string)` | Enable file content cache (100 MB default) |
| `WithBlockCacheDir(dir string)` | Enable HTTP range block cache (50 MB default) |
| `WithRefCacheDir(dir string)` | Enable tag→digest cache (5 MB default) |
| `WithManifestCacheDir(dir string)` | Enable manifest cache (10 MB default) |
| `WithIndexCacheDir(dir string)` | Enable index blob cache (50 MB default) |
| `WithRefCacheTTL(ttl time.Duration)` | Set TTL for reference cache entries (default: 5 min) |

#### Caching Options (Advanced)

For custom cache implementations, use these options with implementations from `core/cache` or `registry/cache`:

| Option | Description |
|--------|-------------|
| `WithContentCache(cache)` | Set custom content cache implementation |
| `WithBlockCache(cache)` | Set custom block cache implementation |
| `WithRefCache(cache)` | Set custom reference cache implementation |
| `WithManifestCache(cache)` | Set custom manifest cache implementation |
| `WithIndexCache(cache)` | Set custom index cache implementation |

#### Policy Options

| Option | Description |
|--------|-------------|
| `WithPolicy(policy Policy)` | Add a policy that must pass for Fetch and Pull |
| `WithPolicies(policies ...Policy)` | Add multiple policies |

#### Cache Size Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultContentCacheSize` | 100 MB | Default content cache size |
| `DefaultBlockCacheSize` | 50 MB | Default block cache size |
| `DefaultIndexCacheSize` | 50 MB | Default index cache size |
| `DefaultManifestCacheSize` | 10 MB | Default manifest cache size |
| `DefaultRefCacheSize` | 5 MB | Default ref cache size |
| `DefaultRefCacheTTL` | 5 min | Default ref cache TTL |

---

### Push Options

```go
type PushOption func(*pushConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `PushWithTags(tags ...string)` | Apply additional tags to the pushed manifest | none |
| `PushWithAnnotations(map[string]string)` | Set custom manifest annotations | auto-generated |
| `PushWithCompression(Compression)` | Set compression algorithm | CompressionNone |
| `PushWithSkipCompression(fns ...SkipCompressionFunc)` | Predicates to skip compression for specific files | none |
| `PushWithChangeDetection(ChangeDetection)` | Verify files didn't change during creation | ChangeDetectionNone |
| `PushWithMaxFiles(n int)` | Limit number of files (0 = default, negative = unlimited) | 200,000 |

---

### Pull Options

```go
type PullOption func(*pullConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `PullWithSkipCache()` | Bypass ref and manifest caches | false |
| `PullWithMaxIndexSize(maxBytes int64)` | Limit index blob size | 8 MB |
| `PullWithMaxFileSize(limit uint64)` | Per-file size limit (0 = unlimited) | 256 MB |
| `PullWithDecoderConcurrency(n int)` | Zstd decoder thread count (negative uses GOMAXPROCS) | 1 |
| `PullWithDecoderLowmem(bool)` | Zstd low-memory mode | false |
| `PullWithVerifyOnClose(bool)` | Hash verification on Close | true |

---

### Fetch Options

```go
type FetchOption func(*fetchConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `FetchWithSkipCache()` | Bypass ref and manifest caches | false |

---

### Types

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
| Path | `string` | File path relative to archive root |
| DataOffset | `uint64` | Byte offset in data blob |
| DataSize | `uint64` | Size in data blob (compressed if applicable) |
| OriginalSize | `uint64` | Uncompressed size |
| Hash | `[]byte` | SHA256 hash of uncompressed content |
| Mode | `fs.FileMode` | File permission bits |
| UID | `uint32` | File owner's user ID |
| GID | `uint32` | File owner's group ID |
| ModTime | `time.Time` | File modification time |
| Compression | `Compression` | Compression algorithm used |

#### EntryView

```go
type EntryView struct {
    // contains filtered or unexported fields
}
```

EntryView provides a read-only view of an index entry. Views alias the index buffer and are only valid while the Blob remains alive.

**Methods:**

| Method | Description |
|--------|-------------|
| `Path() string` | Returns the path as a string |
| `PathBytes() []byte` | Returns the path bytes from the index buffer |
| `HashBytes() []byte` | Returns the SHA256 hash bytes |
| `DataOffset() uint64` | Returns the data blob offset |
| `DataSize() uint64` | Returns the stored size |
| `OriginalSize() uint64` | Returns the uncompressed size |
| `Mode() fs.FileMode` | Returns the file mode bits |
| `UID() uint32` | Returns the user ID |
| `GID() uint32` | Returns the group ID |
| `ModTime() time.Time` | Returns the modification time |
| `Compression() Compression` | Returns the compression algorithm |
| `Entry() Entry` | Returns a fully copied Entry |

#### Compression

```go
type Compression uint8
```

Compression identifies the compression algorithm used for a file.

#### ChangeDetection

```go
type ChangeDetection uint8
```

ChangeDetection controls how strictly file changes are detected during archive creation.

#### SkipCompressionFunc

```go
type SkipCompressionFunc func(path string, info fs.FileInfo) bool
```

SkipCompressionFunc returns true when a file should be stored uncompressed.

#### ByteSource

```go
type ByteSource interface {
    io.ReaderAt
    Size() int64
    SourceID() string
}
```

ByteSource provides random access to the data blob.

#### Policy

```go
type Policy = registry.Policy
```

Policy evaluates whether a manifest is trusted.

---

### Blob Methods

These methods are available on `*Archive` (returned from Pull) and on `*core.Blob` / `*core.BlobFile` from internal packages.

#### Open

```go
func (b *Blob) Open(name string) (fs.File, error)
```

Open implements `fs.FS`. Returns an `fs.File` for reading the named file. The returned file verifies the content hash on Close and returns `ErrHashMismatch` if verification fails.

#### Stat

```go
func (b *Blob) Stat(name string) (fs.FileInfo, error)
```

Stat implements `fs.StatFS`. Returns file info without reading content.

#### ReadFile

```go
func (b *Blob) ReadFile(name string) ([]byte, error)
```

ReadFile implements `fs.ReadFileFS`. Reads and returns entire file contents.

#### ReadDir

```go
func (b *Blob) ReadDir(name string) ([]fs.DirEntry, error)
```

ReadDir implements `fs.ReadDirFS`. Returns directory entries sorted by name.

#### CopyTo

```go
func (b *Blob) CopyTo(destDir string, paths ...string) error
```

CopyTo extracts specific files to a destination directory.

#### CopyToWithOptions

```go
func (b *Blob) CopyToWithOptions(destDir string, paths []string, opts ...CopyOption) error
```

CopyToWithOptions extracts specific files with options.

#### CopyDir

```go
func (b *Blob) CopyDir(destDir, prefix string, opts ...CopyOption) error
```

CopyDir extracts all files under a directory prefix. Use prefix "." for all files.

#### Entry

```go
func (b *Blob) Entry(path string) (EntryView, bool)
```

Entry returns a read-only view of the entry for the given path.

#### Entries

```go
func (b *Blob) Entries() iter.Seq[EntryView]
```

Entries returns an iterator over all entries.

#### EntriesWithPrefix

```go
func (b *Blob) EntriesWithPrefix(prefix string) iter.Seq[EntryView]
```

EntriesWithPrefix returns an iterator over entries with the given prefix.

#### Len

```go
func (b *Blob) Len() int
```

Len returns the number of entries in the archive.

#### Save

```go
func (b *Blob) Save(indexPath, dataPath string) error
```

Save writes the blob's index and data to the specified file paths.

---

### Copy Options

```go
type CopyOption func(*copyConfig)
```

| Option | Description | Default |
|--------|-------------|---------|
| `CopyWithOverwrite(bool)` | Overwrite existing files | false |
| `CopyWithPreserveMode(bool)` | Preserve file permission modes | false |
| `CopyWithPreserveTimes(bool)` | Preserve file modification times | false |
| `CopyWithCleanDest(bool)` | Clear destination before copying (CopyDir only) | false |
| `CopyWithWorkers(n int)` | Worker count (negative = serial, 0 = auto, positive = fixed) | 0 (auto) |
| `CopyWithReadConcurrency(n int)` | Concurrent range reads | 4 |

---

### Helper Functions

#### DefaultSkipCompression

```go
func DefaultSkipCompression(minSize int64) SkipCompressionFunc
```

DefaultSkipCompression returns a predicate that skips small files and known compressed extensions (.jpg, .png, .gz, .zst, etc.).

---

### Constants

#### Compression Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `CompressionNone` | 0 | No compression |
| `CompressionZstd` | 1 | Zstandard compression |

#### ChangeDetection Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `ChangeDetectionNone` | 0 | No change detection |
| `ChangeDetectionStrict` | 1 | Verify files didn't change during creation |

#### File Name Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultIndexName` | "index.blob" | Default index filename |
| `DefaultDataName` | "data.blob" | Default data filename |
| `DefaultMaxFiles` | 200,000 | Default file limit |

---

### Errors

| Error | Description |
|-------|-------------|
| `ErrHashMismatch` | Content hash verification failed |
| `ErrDecompression` | Decompression failed |
| `ErrSizeOverflow` | Byte counts exceed supported limits |
| `ErrSymlink` | Symlink encountered where not allowed |
| `ErrTooManyFiles` | File count exceeded configured limit |
| `ErrNotFound` | Archive does not exist at the reference |
| `ErrInvalidReference` | Reference string is malformed |
| `ErrInvalidManifest` | Manifest is not a valid blob archive manifest |
| `ErrMissingIndex` | Manifest does not contain an index blob |
| `ErrMissingData` | Manifest does not contain a data blob |
| `ErrDigestMismatch` | Content does not match its expected digest |
| `ErrPolicyViolation` | A policy rejected the manifest |
| `ErrReferrersUnsupported` | Referrers are not supported by the registry |

---

## Internal Packages (Advanced)

These packages provide lower-level functionality for advanced use cases. Most users should use the `blob` package above.

---

### Package blob/core

```
import blobcore "github.com/meigma/blob/core"
```

Package core provides archive creation and reading without registry interaction.

#### Key Types

| Type | Description |
|------|-------------|
| `*Blob` | Random access to archive files |
| `*BlobFile` | Wraps `*Blob` with file handle (must be closed) |

#### Key Functions

| Function | Description |
|----------|-------------|
| `New(indexData []byte, source ByteSource, opts ...Option) (*Blob, error)` | Create Blob from index data and byte source |
| `OpenFile(indexPath, dataPath string, opts ...Option) (*BlobFile, error)` | Open local archive files |
| `Create(ctx, dir string, indexW, dataW io.Writer, opts ...CreateOption) error` | Build archive to arbitrary writers |
| `CreateBlob(ctx, srcDir, destDir string, opts ...CreateBlobOption) (*BlobFile, error)` | Create archive to local files |

#### Options

**Blob Options (`Option`):**

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxFileSize(limit uint64)` | Per-file size limit | 256 MB |
| `WithMaxDecoderMemory(limit uint64)` | Zstd decoder memory limit | 256 MB |
| `WithDecoderConcurrency(n int)` | Zstd decoder thread count | 1 |
| `WithDecoderLowmem(bool)` | Zstd low-memory mode | false |
| `WithVerifyOnClose(bool)` | Hash verification on Close | true |
| `WithCache(cache Cache)` | Content cache for file reads | none |

**Create Options (`CreateOption`):**

| Option | Description | Default |
|--------|-------------|---------|
| `CreateWithCompression(Compression)` | Compression algorithm | CompressionNone |
| `CreateWithChangeDetection(ChangeDetection)` | File change detection | ChangeDetectionNone |
| `CreateWithSkipCompression(fns ...SkipCompressionFunc)` | Skip compression predicates | none |
| `CreateWithMaxFiles(n int)` | Maximum file count | 200,000 |

**CreateBlob Options (`CreateBlobOption`):**

| Option | Description | Default |
|--------|-------------|---------|
| `CreateBlobWithIndexName(name string)` | Override index filename | "index.blob" |
| `CreateBlobWithDataName(name string)` | Override data filename | "data.blob" |
| `CreateBlobWithCompression(Compression)` | Compression algorithm | CompressionNone |
| `CreateBlobWithChangeDetection(ChangeDetection)` | File change detection | ChangeDetectionNone |
| `CreateBlobWithSkipCompression(fns ...SkipCompressionFunc)` | Skip compression predicates | none |
| `CreateBlobWithMaxFiles(n int)` | Maximum file count | 200,000 |

---

### Package blob/core/cache

```
import "github.com/meigma/blob/core/cache"
```

Package cache provides content-addressed caching interfaces.

#### Interfaces

**Cache:**

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

**StreamingCache:**

```go
type StreamingCache interface {
    Cache
    Writer(hash []byte) (Writer, error)
}
```

**BlockCache:**

```go
type BlockCache interface {
    Wrap(src ByteSource, opts ...WrapOption) (ByteSource, error)
    MaxBytes() int64
    SizeBytes() int64
    Prune(targetBytes int64) (int64, error)
}
```

---

### Package blob/core/cache/disk

```
import "github.com/meigma/blob/core/cache/disk"
```

Package disk provides disk-backed cache implementations.

#### Functions

| Function | Description |
|----------|-------------|
| `New(dir string, opts ...Option) (*Cache, error)` | Create content cache |
| `NewBlockCache(dir string, opts ...BlockCacheOption) (*BlockCache, error)` | Create block cache |

#### Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxBytes(n int64)` | Maximum cache size | 0 (unlimited) |
| `WithShardPrefixLen(n int)` | Directory sharding | 2 |
| `WithDirPerm(mode os.FileMode)` | Directory permissions | 0700 |
| `WithBlockMaxBytes(n int64)` | Maximum block cache size | 0 (unlimited) |

---

### Package blob/core/http

```
import blobhttp "github.com/meigma/blob/core/http"
```

Package http provides a ByteSource backed by HTTP range requests.

#### Functions

```go
func NewSource(url string, opts ...Option) (*Source, error)
```

NewSource creates a Source backed by HTTP range requests.

#### Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithClient(client *http.Client)` | HTTP client for requests | http.DefaultClient |
| `WithHeaders(headers http.Header)` | Additional headers | none |
| `WithHeader(key, value string)` | Single additional header | none |
| `WithSourceID(id string)` | Override source identifier for cache keys | auto-generated |

---

### Package blob/registry

```
import "github.com/meigma/blob/registry"
```

Package registry provides direct OCI registry operations.

#### Key Types

| Type | Description |
|------|-------------|
| `*Client` | Registry operations client |
| `*BlobManifest` | Archive manifest from registry |

#### Key Functions

| Function | Description |
|----------|-------------|
| `New(opts ...Option) *Client` | Create registry client |

#### Client Methods

| Method | Description |
|--------|-------------|
| `Push(ctx, ref string, b *blob.Blob, opts ...PushOption) error` | Push archive to registry |
| `Pull(ctx, ref string, opts ...PullOption) (*blob.Blob, error)` | Pull archive from registry |
| `Fetch(ctx, ref string, opts ...FetchOption) (*BlobManifest, error)` | Fetch manifest metadata |
| `Tag(ctx, ref, digest string) error` | Create or update a tag |
| `Resolve(ctx, ref string) (string, error)` | Resolve tag to digest |

---

### Package blob/registry/cache

```
import "github.com/meigma/blob/registry/cache"
```

Package cache provides caching interfaces for the registry client.

#### Interfaces

**RefCache:** Caches reference to digest mappings.

**ManifestCache:** Caches digest to manifest mappings.

**IndexCache:** Caches digest to index blob mappings.

---

### Package blob/registry/cache/disk

```
import "github.com/meigma/blob/registry/cache/disk"
```

Package disk provides disk-backed cache implementations for the registry client.

#### Functions

| Function | Description |
|----------|-------------|
| `NewRefCache(dir string, opts ...RefCacheOption) (*RefCache, error)` | Create ref cache |
| `NewManifestCache(dir string, opts ...ManifestCacheOption) (*ManifestCache, error)` | Create manifest cache |
| `NewIndexCache(dir string, opts ...IndexCacheOption) (*IndexCache, error)` | Create index cache |

#### Common Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxBytes(n int64)` | Maximum cache size | 0 (unlimited) |
| `WithShardPrefixLen(n int)` | Directory sharding | 2 |
| `WithDirPerm(mode os.FileMode)` | Directory permissions | 0700 |

#### RefCache Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithRefCacheTTL(ttl time.Duration)` | Time-to-live for entries | 0 (no expiration) |

---

### Package blob/policy/sigstore

```
import "github.com/meigma/blob/policy/sigstore"
```

Package sigstore provides Sigstore signature verification policies.

#### Functions

```go
func NewPolicy(opts ...Option) (Policy, error)
```

NewPolicy creates a Sigstore verification policy.

#### Options

| Option | Description |
|--------|-------------|
| `WithIdentity(issuer, subject string)` | Require signatures from specific issuer/subject |

---

### Package blob/policy/opa

```
import "github.com/meigma/blob/policy/opa"
```

Package opa provides OPA-based policy evaluation for SLSA attestations.

#### Functions

```go
func NewPolicy(opts ...Option) (Policy, error)
```

NewPolicy creates an OPA policy evaluator.

#### Options

| Option | Description |
|--------|-------------|
| `WithPolicyFile(path string)` | Load Rego policy from file |
| `WithPolicyString(rego string)` | Use inline Rego policy |
