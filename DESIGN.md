# Blob Archive Format Design

## Overview

Blob is a file archive format designed specifically for storing and accessing files in OCI 1.1 container registries. Unlike general-purpose archive formats (TAR, ZIP), Blob is optimized for a narrow use case: random access to individual files via HTTP range requests without downloading the entire archive.

## Problem Statement

We want to store collections of files (ranging from 10KB to 100MB) in OCI registries and access them efficiently from remote workloads. The key requirements are:

1. **Random access**: Read any file without streaming the entire archive
2. **Integrity**: Protect against bitrot and tampering throughout the supply chain
3. **Directory fetches**: Efficiently retrieve all files in a directory
4. **Local caching**: Support efficient caching with automatic deduplication

Traditional archive formats fail these requirements:
- TAR requires sequential streaming to locate files
- ZIP has a central directory but wasn't designed for HTTP range requests
- Neither integrates well with OCI's content-addressed model

## Architecture

### Two-Blob Design

The archive consists of two separate OCI blobs:

```
┌─────────────────────────────────────────────────────────┐
│                    OCI Artifact                         │
├─────────────────────────────────────────────────────────┤
│  Manifest (signed via OCI signing)                      │
│    ├── references: index blob digest                    │
│    └── references: data blob digest                     │
├─────────────────────────────────────────────────────────┤
│  Index Blob (~1MB for 10K files)                        │
│    ├── FlatBuffers-encoded file metadata                │
│    ├── Paths, offsets, sizes, hashes                    │
│    └── Sorted by path for binary search                 │
├─────────────────────────────────────────────────────────┤
│  Data Blob (concatenated file contents)                 │
│    ├── Files stored in path-sorted order                │
│    ├── Tightly packed (no padding)                      │
│    └── Optional per-file zstd compression               │
└─────────────────────────────────────────────────────────┘
```

**Why separate blobs?**

- Index is small and cacheable; fetch once, use for all lookups
- Data blob accessed via range requests; never need to download entirely
- Aligns with OCI model where each blob has its own digest
- Manifest binds them together; signing the manifest protects both

### File Access Flow

1. Fetch index blob (small, cacheable by OCI digest)
2. Binary search index for file path → O(log n)
3. Get file's offset, size, and SHA256 hash from entry
4. HTTP range request against data blob for just that file's bytes
5. Decompress if needed
6. Verify SHA256 hash
7. Return content

### Directory Access Flow

Because files are stored sorted by path, all files in a directory are physically adjacent in the data blob:

1. Binary search index for first entry matching prefix
2. Scan forward to collect all entries with that prefix
3. Single range request spanning first entry to last entry
4. Split response by offsets, decompress, verify each file

This means fetching 100 files in `/assets/images/` requires only one HTTP round-trip.

## Integrity Model

### Per-File Hashes

Every file entry includes a SHA256 hash of the uncompressed content. This enables:

- Verification after range request (detect corruption or tampering)
- Content-addressed caching (hash as cache key)
- Precise identification of corrupted files

### OCI Integration

The integrity chain leverages OCI's existing infrastructure:

```
Signature → Manifest → Index Digest → Per-file hashes
                    → Data Digest
```

- OCI manifest contains digests of both blobs
- Manifest can be signed (cosign, notation)
- SLSA provenance attestations can be attached as referrers
- Tampering with any file invalidates the chain

### Why Not Merkle Trees?

Merkle trees would enable compact proofs that a specific file belongs to the archive. We chose simple per-file hashes because:

- OCI signing already provides tamper protection
- Per-file hashes are simpler to implement and debug
- The extra proof capability isn't needed for our use case
- Can be added later if requirements change

## Caching Strategy

### Content-Addressed Cache

Files are cached by their SHA256 hash, not by path:

```
Cache key:   sha256:<hash>
Cache value: decompressed file content
```

**Benefits:**

- Automatic deduplication: same file in different archives = one cache entry
- Cross-archive sharing: common files (libraries, assets) cached once
- Implicit verification: if cache returns content for hash X, it's correct
- No staleness: content-addressed entries are immutable

### Cache Interaction

On cache hit, we skip both network fetch AND hash verification—the hash is the lookup key, so correctness is guaranteed by the cache's integrity.

```
Read file:
  1. Lookup path in index → get hash
  2. Check cache for hash
  3. HIT:  return cached content (no verification needed)
  4. MISS: fetch → decompress → verify → cache → return
```

### Prefetching

The API supports warming the cache:

- `Prefetch(paths...)`: fetch specific files, batch adjacent ones
- `PrefetchDir(prefix)`: fetch entire directory with one range request

## Compression

### Per-File Compression

Each file can be independently compressed or stored raw:

- Compression algorithm stored in index entry
- Currently supported: none, zstd
- Extensible enum for future algorithms

**Why per-file?**

- Random access: can decompress one file without touching others
- Flexibility: compress large text files, skip already-compressed images
- Streaming: decompress as data arrives from range request

### Algorithm Choice

Zstd is the primary compression algorithm because:

- Excellent compression ratio
- Fast decompression (critical for access latency)
- Dictionary support for future optimization
- Widely adopted, well-maintained

## File Metadata

Each entry preserves:

- Path (relative to archive root)
- Size (both compressed and original)
- SHA256 hash
- Unix permissions (mode)
- Owner (uid, gid)
- Modification time (nanosecond precision)
- Compression algorithm

### What We Don't Preserve

- Empty directories (not needed; directories are implicit in paths)
- Symbolic links (out of scope for v1; files only)
- Extended attributes (not needed for target use case)
- Hard links (files are deduplicated by content at cache layer instead)

## Index Format

### FlatBuffers

The index uses Google FlatBuffers for serialization:

- Zero-copy access: read fields directly from buffer, no parsing
- O(1) random access to entries by index
- Built-in binary search via `key` attribute
- Schema evolution for future compatibility
- Small overhead, efficient encoding

### Why Not Custom Binary?

FlatBuffers provides the zero-copy performance of a custom format with:

- Well-tested implementation
- Schema definition and evolution
- Generated code for type safety
- Debugging tools

### Why Not Protobuf/JSON?

- Protobuf requires full deserialization before access
- JSON has parsing overhead and larger size
- Neither supports zero-copy access patterns

## What We're Optimizing For

1. **Random file access via HTTP**: Single file read = index lookup + one range request
2. **Directory fetches**: Adjacent storage enables single request for entire directories
3. **Integrity verification**: Per-file hashes catch corruption at the granularity that matters
4. **Cache efficiency**: Content-addressed caching with automatic deduplication
5. **OCI integration**: Two-blob design aligns with OCI model and signing

## What We're Not Optimizing For

1. **General-purpose archiving**: This is not a TAR/ZIP replacement
2. **Embedded index**: Index-in-data-blob is not supported (complicates range math)
3. **Streaming writes**: Writer requires complete directory upfront
4. **Non-OCI use cases**: Design assumes OCI registry as primary backend
5. **Symbolic links**: Out of scope for v1
6. **Maximum density**: No deduplication within a single archive (cache handles cross-archive)

## Future Considerations

These are explicitly out of scope for v1 but the design does not preclude them:

- **Symbolic link support**: Add entry type field, store target path
- **Per-file compression predicates**: Compress based on file type/size
- **Chunk-level integrity**: For very large files, per-chunk hashes
- **Embedded index mode**: Index at start of data blob for single-blob scenarios
- **Encryption**: Per-file encryption with key management
