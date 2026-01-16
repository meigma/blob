---
sidebar_position: 1
---

# Architecture

Understanding the design decisions behind blob.

## Two-Blob Design

The blob archive format splits its content across two separate OCI blobs: a small index blob containing file metadata, and a large data blob containing the actual file contents. This separation is fundamental to how blob achieves efficient random access.

### Why Separate Index and Data?

The index blob is compact. For an archive containing 10,000 files, the index typically measures around 1MB. This small size means clients can fetch the entire index in a single request, cache it locally, and use it for all subsequent file lookups without any additional network traffic.

The data blob, by contrast, may be gigabytes in size. By keeping it separate, clients never need to download it entirely. Instead, they issue HTTP range requests to fetch only the specific byte ranges they need. A client reading a single 50KB file from a 2GB archive transfers roughly 50KB of data, not 2GB.

This design also aligns naturally with OCI's content-addressed model. Each blob has its own digest, and the OCI manifest binds them together. When the manifest is signed using tools like cosign or notation, that signature protects both blobs simultaneously. The integrity chain flows from the signature through the manifest digests down to the individual file hashes.

### Trade-offs

Separating index and data means two HTTP requests are required before reading any file content: one to fetch the index, one to fetch the file's bytes. For use cases that read many files repeatedly, the index fetch amortizes quickly. For use cases that read exactly one file exactly once, the two-request minimum adds overhead compared to a single-blob format with an embedded index.

The design explicitly accepts this trade-off. Blob targets scenarios where the index is fetched once and reused for many file accesses, whether across multiple program runs via caching or within a single session that reads multiple files.

## Path-Sorted Storage

Files in the data blob are stored in lexicographical order by their paths. This ordering is not arbitrary; it enables efficient directory fetches.

### Why Sort by Path?

When files are sorted by path, all files within a directory end up physically adjacent in the data blob. Consider a directory `/assets/images/` containing 100 image files. With path-sorted storage, these 100 files occupy a contiguous byte range in the data blob. Fetching the entire directory requires just one HTTP range request spanning from the first file's offset to the last file's end.

Without this sorting, the same 100 files might be scattered throughout the data blob. Fetching them would require up to 100 separate range requests, each with its own network round-trip latency. Even with HTTP/2 multiplexing, the overhead adds up.

The index exploits this ordering too. Because entries are sorted, the index can use binary search for O(log n) lookups. Finding a file among 10,000 entries requires at most 14 comparisons, regardless of where in the archive the file appears.

### Finding Directory Contents

To enumerate a directory's contents, the index performs a binary search to find the first entry matching the directory prefix, then scans forward collecting entries until the prefix no longer matches. This scan is cheap because entries are already sorted; no additional sorting or filtering is required.

This same pattern powers the `PrefetchDir` operation. The cache wrapper can warm its cache with an entire directory's contents using a single range request, then split the response into individual files for caching.

## FlatBuffers Index

The index uses Google FlatBuffers for serialization, a choice that prioritizes read performance over write convenience.

### Why FlatBuffers?

FlatBuffers provides zero-copy access to structured data. When a client loads the index blob, it does not need to parse, allocate, or deserialize anything. The FlatBuffers library reads field values directly from the raw byte buffer, following offsets to locate data. This means index access is essentially pointer arithmetic plus bounds checking.

The format also supports O(1) random access to array elements. Given an index offset, locating the Nth entry requires no iteration through preceding entries. Combined with the binary search capability via FlatBuffers' `key` attribute on the path field, this enables fast lookups regardless of archive size.

Schema evolution is another benefit. FlatBuffers schemas can add new fields without breaking compatibility with older readers. A client built against schema version 1 can read an index written with schema version 2, ignoring fields it does not understand. This provides a migration path for adding capabilities like new compression algorithms or metadata fields.

### Why Not Protocol Buffers?

Protocol Buffers require full deserialization before access. Reading a single field from a protobuf message means parsing the entire message into memory objects. For an index with 10,000 entries where you need entry 5,000, protobuf must process all 10,000 entries. FlatBuffers jumps directly to entry 5,000.

Protocol Buffers also allocate heavily during deserialization. Each message becomes a language-level object with its own memory allocation. For a large index, this creates garbage collection pressure that FlatBuffers avoids entirely.

### Why Not JSON or Custom Binary?

JSON has significant parsing overhead and produces larger serialized sizes. A JSON representation of the index would take longer to parse and consume more bandwidth to transfer. For a format optimized around network efficiency, this overhead is unacceptable.

A custom binary format could match FlatBuffers' performance but would sacrifice schema evolution, type safety, and tooling support. FlatBuffers provides well-tested implementations in multiple languages, schema compilation, debugging tools, and a specification that others can implement. Building and maintaining all of that for a custom format would be substantial effort for minimal benefit.

## Per-File Compression

Each file in the archive can be independently compressed or stored uncompressed. The compression algorithm is recorded in the entry's metadata, and currently blob supports no compression and zstd compression.

### Why Compress Files Individually?

Per-file compression preserves random access. To decompress a single file, the client reads only that file's compressed bytes and decompresses them. The compression state of other files in the archive is irrelevant.

If blob used whole-archive compression, reading a file near the end of the archive might require decompressing everything before it, depending on the algorithm. Streaming decompressors cannot seek; they must process bytes in order. This would undermine the entire premise of random access via range requests.

Per-file compression also enables intelligent decisions about what to compress. Text files, JSON documents, and source code compress well. JPEG images, PNG files, and already-compressed content do not benefit from additional compression and may even grow slightly. By making compression decisions per-file, archives can skip compression for content that would not benefit while still compressing content that would.

### Why Zstd?

Zstd offers an excellent balance of compression ratio and decompression speed. Its decompression is faster than gzip while achieving better compression ratios. For a format where decompression happens on every file read, fast decompression directly impacts access latency.

Zstd also supports dictionary compression, which could enable future optimizations. If many files share common patterns, a dictionary trained on those patterns could improve compression ratios substantially. The current implementation does not use dictionaries, but the choice of zstd keeps this door open.

The format uses an extensible compression enum, so adding new algorithms later requires only defining new enum values and implementing the corresponding compressor and decompressor. Existing archives with existing compression settings remain readable.

## What Blob Optimizes For

Blob makes deliberate trade-offs in favor of specific use cases.

**Random file access via HTTP.** The entire design centers on enabling efficient access to individual files through HTTP range requests. Every architectural decision supports this goal.

**Directory fetches.** Path-sorted storage ensures that common access patterns like "read all files in this directory" map to efficient single-request operations.

**Integrity verification.** Per-file SHA256 hashes enable verification of individual files without trusting or verifying unrelated content. Combined with OCI signing, this provides a complete integrity chain from source to consumption.

**Cache efficiency.** Content-addressed caching with automatic deduplication means identical files across different archives share cache storage. The cache key is the content hash, not the file path or archive identity.

**OCI integration.** The two-blob design fits naturally into OCI's model of content-addressed blobs referenced by manifests. Standard OCI tooling, registries, and signing workflows work without modification.

## What Blob Does Not Optimize For

Understanding what blob explicitly does not target helps clarify its design boundaries.

**General-purpose archiving.** Blob is not a replacement for tar, zip, or other general-purpose archive formats. It targets a specific use case: storing file collections in OCI registries for random access. Using blob for local backup or file distribution without OCI infrastructure would miss the point.

**Embedded index.** Some archive formats embed the index within the data blob, either at the beginning or end. Blob separates them because embedding complicates range request math and prevents caching the index independently from the data. The two-request minimum is an accepted cost.

**Streaming writes.** Creating a blob archive requires knowing all files upfront. The writer needs complete information to sort entries by path and compute their final offsets. Streaming writes where files arrive incrementally would require different trade-offs.

**Non-OCI use cases.** While technically possible to use blob archives outside OCI registries, the design assumes OCI semantics: content-addressed blobs, manifests binding blobs together, and registries supporting range requests.

**Symbolic links.** The current version does not support symbolic links. This simplifies the format and avoids complexity around link targets, cycles, and security considerations. Files only.

**Within-archive deduplication.** If the same file appears twice within a single archive under different paths, blob stores it twice. Deduplication happens at the cache layer across archives, not within a single archive. This keeps the format simple and predictable.

## Comparison with Alternatives

Several other formats attempt to solve similar problems. Understanding why blob makes different trade-offs illuminates its design rationale.

### TAR

TAR stores files sequentially with metadata interleaved. Finding a file requires streaming through the archive until encountering that file's header. For a file near the end of a large archive, this means reading nearly the entire archive. TAR was designed for tape drives where sequential access was the only option; it does not adapt well to random access via HTTP.

### ZIP

ZIP includes a central directory at the end of the archive, enabling random access without full sequential reads. However, ZIP was designed before HTTP range requests were common, and its central directory structure was not optimized for this access pattern. ZIP also uses a different compression model and lacks the path-sorted storage that enables efficient directory fetches.

### eStargz

eStargz (estargz) extends the stargz format with an index that enables lazy pulling of container layers. It targets container image layers specifically and optimizes for the container runtime use case. Blob targets a different use case: arbitrary file collections in OCI registries, accessed by applications rather than container runtimes.

### SOCI

SOCI (Seekable OCI) provides lazy loading for container images through separate index artifacts. Like eStargz, it focuses on container image layers. SOCI uses a different architecture, storing indices as separate OCI artifacts rather than modifying the layer format.

### Nydus

Nydus is a container image format designed for lazy pulling with additional features like chunk deduplication and encryption. It targets container images and integrates deeply with container runtimes. Blob has a narrower focus on file archives with simpler requirements.

Each of these formats makes different trade-offs for different use cases. Blob's trade-offs favor simplicity, OCI integration, and the specific access patterns of file archives accessed by applications rather than container runtimes.
