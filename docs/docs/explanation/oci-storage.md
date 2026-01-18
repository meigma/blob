---
sidebar_position: 3
---

# OCI Storage

Understanding how blob archives are stored in OCI container registries.

## Why OCI Registries?

OCI (Open Container Initiative) registries provide the ideal storage backend for blob archives:

- **Content-addressed storage**: Every blob is identified by its SHA256 digest
- **HTTP range requests**: Registries support partial blob downloads
- **Authentication**: Standard OAuth2/bearer token flows
- **Distribution**: Registries handle replication, caching, and CDN integration
- **Signing**: Native support for cosign, notation, and other signing tools
- **Ubiquity**: Available from every major cloud provider and self-hostable

Blob archives are designed specifically for this environment, optimizing for lazy access via range requests while fitting naturally into OCI's content model.

## The OCI Artifact Model

OCI registries store content as **blobs** referenced by **manifests**. A manifest is a JSON document that lists the blobs comprising an artifact and includes metadata like annotations.

### References

Content is accessed via references in the form:

```
registry/repository:tag
registry/repository@sha256:digest
```

Tags are mutable pointers (like Git branches). Digests are immutable content addresses (like Git commits).

### Content Addressing

Every blob and manifest has a digest computed from its content:

```
sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4
```

This enables:
- Deduplication: identical content is stored once
- Verification: content can be validated against its digest
- Caching: content never changes, so caches never become stale

## Blob Archive as OCI Artifact

A blob archive is stored as an OCI 1.1 artifact with a custom artifact type and two layer blobs.

### Manifest Structure

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.meigma.blob.v1",
  "config": {
    "mediaType": "application/vnd.oci.empty.v1+json",
    "digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
    "size": 2
  },
  "layers": [
    {
      "mediaType": "application/vnd.meigma.blob.index.v1+flatbuffers",
      "digest": "sha256:...",
      "size": 1048576
    },
    {
      "mediaType": "application/vnd.meigma.blob.data.v1",
      "digest": "sha256:...",
      "size": 104857600
    }
  ],
  "annotations": {
    "org.opencontainers.image.created": "2024-01-15T10:30:00Z"
  }
}
```

### Artifact Type

```
application/vnd.meigma.blob.v1
```

This identifies the manifest as a blob archive, distinct from container images or other OCI artifacts.

### Config Blob

OCI manifests require a config blob. Blob archives use an empty JSON object (`{}`) with the standard empty media type. This satisfies the spec while keeping the config minimal.

### Layer Media Types

| Layer | Media Type | Description |
|-------|------------|-------------|
| Index | `application/vnd.meigma.blob.index.v1+flatbuffers` | FlatBuffers-encoded file metadata |
| Data | `application/vnd.meigma.blob.data.v1` | Concatenated file contents |

## How Range Requests Work

OCI registries expose blobs via a standard HTTP endpoint:

```
GET /v2/{repository}/blobs/{digest}
```

This endpoint supports HTTP Range headers, enabling partial downloads:

```http
GET /v2/myorg/myarchive/blobs/sha256:abc123 HTTP/1.1
Host: ghcr.io
Authorization: Bearer <token>
Range: bytes=1000-1999
```

```http
HTTP/1.1 206 Partial Content
Content-Range: bytes 1000-1999/104857600
Content-Length: 1000

<1000 bytes of data>
```

### Lazy Data Loading

When you pull a blob archive:

1. **Fetch manifest**: Small JSON (~1 KB), identifies index and data blobs
2. **Fetch index blob**: Small (~1 MB), contains all file metadata
3. **Data blob is NOT fetched**: Only a URL is constructed

When you read a file:

1. **Lookup in index**: O(log n) binary search for file entry
2. **Range request**: Fetch only the bytes for that file
3. **Decompress if needed**: Zstd decompression is per-file
4. **Verify hash**: SHA256 matches index entry

This means reading a 50 KB file from a 2 GB archive transfers only ~51 MB total (manifest + index + file), not 2 GB.

### Authentication Flow

Range requests require authentication. The client handles this automatically:

1. **Credential lookup**: From Docker config or provided credentials
2. **Token exchange**: OAuth2 flow if needed (handled by ORAS)
3. **Header injection**: Bearer token added to range requests
4. **Token refresh**: Automatic refresh on 401 responses

## Integrity Chain

Blob archives provide end-to-end integrity verification:

```
┌──────────────────────────────────────────────────────────────┐
│                     Integrity Chain                          │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌────────────┐    ┌────────────┐    ┌────────────────────┐  │
│  │ Signature  │───▶│  Manifest  │───▶│      Blobs         │  │
│  │ (cosign)   │    │  (digest)  │    │ (index + data)     │  │
│  └────────────┘    └────────────┘    └────────────────────┘  │
│                           │                    │             │
│                           ▼                    ▼             │
│                    ┌──────────────────────────────────────┐  │
│                    │           Per-File Hashes           │  │
│                    │    (SHA256 in index for each file)  │  │
│                    └──────────────────────────────────────┘  │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

1. **Optional signature** (cosign/notation) signs the manifest digest
2. **Manifest** contains digests for index and data blobs
3. **Index blob** contains SHA256 hashes for each file
4. **File reads** verify content against per-file hashes

This means:
- Tampering with any blob invalidates the manifest
- Tampering with the manifest invalidates any signature
- Tampering with file content is detected on read

## OCI Signing Compatibility

Blob archives work with standard OCI signing tools:

### Cosign

```bash
# Sign the manifest
cosign sign ghcr.io/myorg/myarchive:v1.0.0

# Verify signature
cosign verify ghcr.io/myorg/myarchive:v1.0.0
```

### Notation

```bash
# Sign the manifest
notation sign ghcr.io/myorg/myarchive:v1.0.0

# Verify signature
notation verify ghcr.io/myorg/myarchive:v1.0.0
```

The signature covers the manifest digest, which transitively covers all blob digests and file hashes.

## Cache Architecture

Multiple cache layers optimize repeated access:

```
┌───────────────────────────────────────────────────────────────┐
│                    Complete Cache Stack                       │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────────── OCI Client Caches ───────────────────┐  │
│  │  RefCache → ManifestCache → IndexCache                  │  │
│  │  (tag→digest)  (digest→manifest)  (digest→index)        │  │
│  └─────────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  ┌─────────────────── File Content Cache ──────────────────┐  │
│  │  ContentCache: hash → uncompressed file content         │  │
│  │  (deduplicates across archives)                         │  │
│  └─────────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  ┌─────────────────── Block Cache ─────────────────────────┐  │
│  │  BlockCache: source+offset → raw data blocks            │  │
│  │  (optimizes random HTTP access)                         │  │
│  └─────────────────────────────────────────────────────────┘  │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

Each layer serves a different purpose:

| Cache | Purpose | Key |
|-------|---------|-----|
| RefCache | Avoid tag resolution requests | ref string |
| ManifestCache | Avoid manifest fetch requests | manifest digest |
| IndexCache | Avoid index blob downloads | index digest |
| ContentCache | Deduplicate file content | file content hash |
| BlockCache | Reduce HTTP requests | source + block index |

## Why This Architecture

The design makes specific trade-offs:

### Small Index = Fast Metadata Access

The index blob is compact (typically 1 MB for 10,000 files). This means:
- Quick download on first access
- Fits in memory for O(log n) lookups
- Cacheable with minimal storage

### Lazy Data = Minimal Bandwidth

The data blob is never downloaded entirely. This means:
- Reading one file transfers only that file's bytes
- Network cost scales with what you actually read
- Works over slow connections

### Multi-Level Caching = Optimized Repeated Access

The cache stack minimizes redundant work:
- Second pull of same tag: no network requests (all cached)
- Same file in different archives: content cache hit
- Random reads in same data blob: block cache coalesces requests

### Standard OCI = Universal Compatibility

Using OCI means:
- Works with any OCI-compliant registry
- Standard tools (crane, skopeo, cosign) work as expected
- No custom server software required
- Existing infrastructure (CDNs, mirrors) applies

## See Also

- [Architecture](architecture) - Overall blob archive design
- [Integrity](integrity) - Detailed verification mechanisms
- [OCI Client](../guides/oci-client) - Push and pull operations
- [OCI Client Caching](../guides/oci-client-caching) - Client cache configuration
