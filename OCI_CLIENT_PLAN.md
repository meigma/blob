# OCI Client Layer Implementation Plan

## Overview

Build a specialized OCI client for pushing and pulling blob archives to/from OCI registries. The client will be a **separate Go module** under `client/` to avoid pulling OCI dependencies into the core library.

## Design Decisions

| Decision | Choice |
|----------|--------|
| OCI library | oras-go (oras.land/oras-go/v2) |
| OCI version | Target 1.1 artifact manifests, probe and fallback to 1.0 |
| Media types | Custom vendor types (application/vnd.meigma.blob.*) |
| Authentication | Docker config.json by default, explicit credentials supported |
| Pull behavior | Lazy by default (Blob backed by registry via HTTP range requests) |
| Cache | Separate interface for refs/manifests (not content cache) |

## API

```go
client := client.New(opts...)

// Push archive to registry (streams from Blob, tags multiple refs)
err := client.Push(ctx, "registry.com/repo:v1.0.0", blob,
    client.WithTags("latest"))

// Fetch manifest only (no data download)
manifest, err := client.Fetch(ctx, "registry.com/repo:v1.0.0")

// Pull lazy Blob backed by registry
blob, err := client.Pull(ctx, "registry.com/repo:v1.0.0")

// Tag existing manifest
err := client.Tag(ctx, "registry.com/repo:latest", "sha256:abc...")
```

## Existing Root Module APIs

The following APIs are already available for the client to use:

### Blob Methods
```go
// IndexData returns the raw FlatBuffers-encoded index data.
func (b *Blob) IndexData() []byte

// Stream returns a reader that streams the entire data blob.
func (b *Blob) Stream() io.Reader

// Size returns the total size of the data blob in bytes.
func (b *Blob) Size() int64

// Save writes the blob archive to the specified paths (atomic writes).
func (b *Blob) Save(indexPath, dataPath string) error
```

### File-Based Operations
```go
// BlobFile wraps a Blob with its underlying data file handle.
type BlobFile struct {
    *Blob
    // ...
}

// OpenFile opens a blob archive from index and data files.
func OpenFile(indexPath, dataPath string, opts ...Option) (*BlobFile, error)

// CreateBlob creates a blob archive from srcDir and writes it to destDir.
func CreateBlob(ctx context.Context, srcDir, destDir string, opts ...CreateBlobOption) (*BlobFile, error)
```

## Required Root Module Change

### Add `http.WithHeaders()` option (`http/source.go`)

```go
// WithHeaders sets additional HTTP headers for all requests.
// Used by client.Pull() to pass authentication headers.
func WithHeaders(headers http.Header) Option {
    return func(s *Source) {
        s.headers = headers
    }
}
```

## Client Module Structure

```
client/
├── go.mod                      # Separate module
├── client.go                   # Client type, New() - high-level blob archive API
├── client_opts.go              # WithCredentials, WithDockerConfig, etc.
├── push.go                     # Push implementation (uses OCIClient)
├── push_opts.go                # WithTags, WithAnnotations
├── pull.go                     # Pull implementation (lazy Blob)
├── pull_opts.go                # PullOption
├── fetch.go                    # Fetch manifest only
├── fetch_opts.go               # FetchOption
├── tag.go                      # Tag implementation
├── manifest.go                 # BlobManifest type
├── media_types.go              # Media type constants
├── errors.go                   # Sentinel errors
├── cache.go                    # RefCache, ManifestCache interfaces
├── oci/                        # Generic OCI layer (wraps ORAS)
│   ├── client.go               # OCIClient type
│   ├── client_opts.go          # OCIClient options
│   ├── auth.go                 # Auth helpers
│   ├── auth_docker.go          # Docker config.json reader
│   └── errors.go               # OCI-specific errors
└── *_test.go
```

### Layer Responsibilities

**`client/oci.OCIClient`** - Generic OCI operations (blob-agnostic):
- `PushBlob(ctx, repo, reader, size, mediaType) (Descriptor, error)`
- `FetchBlob(ctx, repo, digest) (io.ReadCloser, error)`
- `PushManifest(ctx, repo, manifest) (Descriptor, error)`
- `FetchManifest(ctx, repo, desc) (Manifest, error)`
- `Resolve(ctx, repo, ref) (Descriptor, error)`
- `Tag(ctx, repo, digest, tag) error`
- Handles OCI 1.0/1.1 fallback internally (transparent to caller)
- Manages authentication (docker config + explicit credentials)

**`client.Client`** - High-level blob archive operations:
- `Push(ctx, ref, blob, opts...)` - composes OCIClient calls for our two-blob format
- `Pull(ctx, ref, opts...)` - fetches manifest, creates lazy Blob
- `Fetch(ctx, ref, opts...)` - manifest-only retrieval
- `Tag(ctx, ref, digest, opts...)` - delegates to OCIClient
- Manages ref/manifest caching

## Key Types

### Media Types (`client/media_types.go`)
```go
const (
    ArtifactType   = "application/vnd.meigma.blob.v1"
    MediaTypeIndex = "application/vnd.meigma.blob.index.v1+flatbuffers"
    MediaTypeData  = "application/vnd.meigma.blob.data.v1"
)
```

### BlobManifest (`client/manifest.go`)
```go
type BlobManifest struct {
    // Wraps ocispec.Manifest
    // Methods: IndexDescriptor(), DataDescriptor(), Annotations(), Created(), Digest()
}
```

### Cache Interfaces (`client/cache.go`)
```go
type RefCache interface {
    GetDigest(ref string) (string, bool)
    PutDigest(ref string, digest string)
}

type ManifestCache interface {
    GetManifest(digest string) (*BlobManifest, bool)
    PutManifest(digest string, manifest *BlobManifest)
}
```

## Implementation Details

### Push Flow (`client.Client.Push`)
1. Get index bytes via `blob.IndexData()` (size = `len(indexData)`)
2. Get data reader via `blob.Stream()` and size via `blob.Size()`
3. Call `ociClient.PushBlob()` for index blob
4. Call `ociClient.PushBlob()` for data blob (streaming)
5. Build manifest with both descriptors
6. Call `ociClient.PushManifest()` (handles 1.0/1.1 internally)
7. Call `ociClient.Tag()` for additional tags from `WithTags()`

### Pull Flow (`client.Client.Pull`)
1. Fetch manifest via `Fetch()`
2. Call `ociClient.FetchBlob()` for index blob (small, download fully)
3. Construct blob URL for data blob
4. Get auth headers from OCIClient
5. Create `http.Source` with auth headers pointing to data blob URL
6. Return `blob.New(indexData, httpSource)`

### OCI 1.0/1.1 Fallback (`client/oci.OCIClient`)
Handled transparently within OCIClient:
- `PushManifest()`: try 1.1 artifact manifest first, fall back to 1.0 image manifest on rejection
- `FetchManifest()`: detect manifest type from mediaType/artifactType fields, handle both

## Implementation Phases

### Phase 1: Root Module Change
- [ ] Add `WithHeaders()` to http.Source

### Phase 2: OCI Layer (`client/oci/`)
- [ ] Create `client/go.mod` with ORAS dependency
- [ ] Implement `OCIClient` type with options
- [ ] Implement Docker config.json authentication
- [ ] Implement `PushBlob()`, `FetchBlob()`
- [ ] Implement `PushManifest()`, `FetchManifest()` with OCI 1.0/1.1 fallback
- [ ] Implement `Resolve()`, `Tag()`
- [ ] Implement OCI-specific errors

### Phase 3: Client Layer (`client/`)
- [ ] Implement media types
- [ ] Implement `Client` type with options (wraps OCIClient)
- [ ] Implement `BlobManifest` type
- [ ] Implement cache interfaces

### Phase 4: Push and Tag
- [ ] Implement `Push()` using OCIClient primitives
- [ ] Implement `WithTags()` for multi-tag support
- [ ] Implement `Tag()` delegating to OCIClient

### Phase 5: Fetch and Pull
- [ ] Implement `Fetch()` for manifest-only retrieval
- [ ] Implement `Pull()` with lazy HTTP source
- [ ] Handle auth header forwarding to http.Source

### Phase 6: Testing
- [ ] Unit tests for OCIClient with mocked ORAS
- [ ] Unit tests for Client with mocked OCIClient
- [ ] Integration tests with local registry container

## Critical Files

| File | Changes |
|------|---------|
| `http/source.go` | Add `WithHeaders()` option |
| `client/oci/` (new) | Generic OCI layer wrapping ORAS |
| `client/` (new) | High-level blob archive client |

## Verification

1. **Unit tests**: Run `go test ./...` in both root and client modules
2. **Integration test**: Push/pull roundtrip with local registry
   ```bash
   docker run -d -p 5000:5000 registry:2
   go test -tags=integration ./client/...
   ```
3. **Lint**: Run `just ci` to verify all checks pass
