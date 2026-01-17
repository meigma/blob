---
sidebar_position: 5
---

# Performance Tuning

How to tune blob for production workloads.

## When to Tune

The default settings work well for most use cases:
- Archives with files under 256 MB
- Moderate concurrency (1-4 workers)
- Local or low-latency remote sources

Consider tuning when:
- Working with very large files (> 256 MB)
- Running on memory-constrained systems
- Accessing high-latency remote sources
- Extracting to slow storage (network filesystems, HDDs)

## Decoder Options

These options configure the blob reader and affect all read operations.

### Maximum File Size

To allow larger individual files:

```go
archive, err := blob.New(indexData, source,
	blob.WithMaxFileSize(512 << 20), // 512 MB limit
)
```

The default is 256 MB. Set to 0 to disable the limit entirely (not recommended for untrusted archives).

### Maximum Decoder Memory

To limit memory used by the zstd decompressor:

```go
archive, err := blob.New(indexData, source,
	blob.WithMaxDecoderMemory(128 << 20), // 128 MB limit
)
```

The default is 256 MB. Lower values reduce memory usage but may fail on archives compressed with high window sizes.

### Decoder Concurrency

To control zstd decoder parallelism:

```go
archive, err := blob.New(indexData, source,
	blob.WithDecoderConcurrency(2), // 2 goroutines per decoder
)
```

Values:
- `1` (default) - Single-threaded decoding, lowest memory usage
- `0` - Use GOMAXPROCS goroutines (maximum parallelism)
- `> 1` - Use exactly N goroutines

Higher concurrency improves decompression throughput for large files but increases memory usage.

### Low Memory Mode

To reduce memory usage at the cost of decompression speed:

```go
archive, err := blob.New(indexData, source,
	blob.WithDecoderLowmem(true),
)
```

This mode uses approximately 3x less memory but decompresses about 2x slower.

## Verification Options

### Skip Verification on Close

By default, closing a file without reading to EOF drains remaining data to verify the hash. To skip this:

```go
archive, err := blob.New(indexData, source,
	blob.WithVerifyOnClose(false),
)
```

When disabled:
- `Close()` returns immediately without reading remaining data
- Integrity is only guaranteed when callers read to EOF
- Use when you intentionally read partial files

**Warning**: Partial reads may return unverified data. Only disable if your use case does not require full integrity verification.

## Extraction Parallelism

These options control parallel extraction via `CopyTo` and `CopyDir`.

### Worker Count

Controls parallel file writers:

```go
err := archive.CopyDir("/dest", ".",
	blob.CopyWithWorkers(8),
)
```

Guidelines:
- **SSD/fast storage**: 4-8 workers (I/O is rarely the bottleneck)
- **HDD/slow storage**: 1-2 workers (reduce seek overhead)
- **Network filesystem**: 4-16 workers (hide latency)
- **CPU-bound decompression**: Match core count

Use 0 for automatic heuristics or negative values for serial processing.

### Read Concurrency

Controls concurrent range requests for remote archives:

```go
err := archive.CopyDir("/dest", ".",
	blob.CopyWithReadConcurrency(8),
)
```

The default is 4. Higher values:
- Reduce time waiting for network round trips
- Increase memory usage (buffered responses)
- May trigger rate limiting on some servers

For high-latency connections (> 100ms), try 8-16 concurrent reads.

### Read-Ahead Budget

Limits memory used by buffered parallel reads:

```go
err := archive.CopyDir("/dest", ".",
	blob.CopyWithReadAheadBytes(32 << 20), // 32 MB
)
```

Set to 0 to disable the budget (unlimited). Useful when:
- Extracting archives with large files
- Running on memory-constrained systems
- You need predictable memory usage

## Cache Prefetch Concurrency

Controls parallel prefetch workers for cached blobs:

```go
import "github.com/meigma/blob/cache"

cached := cache.New(base, diskCache,
	cache.WithPrefetchConcurrency(8),
)
```

The default is serial (1 worker). Higher values:
- Reduce prefetch time for many small files
- Increase concurrent network requests
- May saturate network or storage bandwidth

## Block Cache Tuning

For remote sources with scattered random reads, block caching can significantly reduce latency.

### Block Size

Smaller blocks reduce wasted bytes but increase metadata overhead. Larger blocks improve sequential read performance:

```go
blockCache, err := disk.NewBlockCache(dir,
    disk.WithBlockMaxBytes(256 << 20),
)

// Small files or fine-grained access
cachedSource, err := blockCache.Wrap(source,
    cache.WithBlockSize(16 << 10),  // 16 KB
)

// Large files or coarser access
cachedSource, err := blockCache.Wrap(source,
    cache.WithBlockSize(256 << 10), // 256 KB
)
```

### Bypass Threshold

Adjust `MaxBlocksPerRead` based on your access patterns:

```go
// Lower threshold: bypass cache more aggressively for sequential reads
cachedSource, err := blockCache.Wrap(source,
    cache.WithMaxBlocksPerRead(2),  // Bypass reads > 2 blocks
)

// Disable bypass: cache everything (useful for small archives)
cachedSource, err := blockCache.Wrap(source,
    cache.WithMaxBlocksPerRead(0),  // Cache all reads
)
```

## Tuning Scenarios

### Memory-Constrained Environment

For systems with limited RAM (< 512 MB available):

```go
archive, err := blob.New(indexData, source,
	blob.WithMaxDecoderMemory(64 << 20),  // 64 MB decoder limit
	blob.WithDecoderLowmem(true),         // Low memory decompression
	blob.WithDecoderConcurrency(1),       // Single-threaded
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithWorkers(2),              // Few parallel writers
	blob.CopyWithReadConcurrency(2),      // Few parallel reads
	blob.CopyWithReadAheadBytes(16 << 20), // 16 MB read budget
)
```

### High-Latency Remote Source

For archives on slow networks (> 200ms RTT):

```go
archive, err := blob.New(indexData, source,
	blob.WithDecoderConcurrency(0),       // Max decoder parallelism
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithWorkers(4),              // Parallel file writing
	blob.CopyWithReadConcurrency(16),     // Many concurrent requests
)
```

### Maximum Throughput

For fastest possible extraction on capable hardware:

```go
archive, err := blob.New(indexData, source,
	blob.WithDecoderConcurrency(0),       // Use all cores
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithWorkers(0),              // Auto-detect workers
	blob.CopyWithReadConcurrency(8),      // Parallel reads
	blob.CopyWithCleanDest(true),         // Skip temp files
)
```

### Reliable CI/CD Pipeline

For production builds prioritizing correctness:

```go
// Archive creation with CreateBlob (recommended)
blobFile, err := blob.CreateBlob(ctx, srcDir, destDir,
	blob.CreateBlobWithCompression(blob.CompressionZstd),
	blob.CreateBlobWithSkipCompression(blob.DefaultSkipCompression(1024)),
	blob.CreateBlobWithChangeDetection(blob.ChangeDetectionStrict),
	blob.CreateBlobWithMaxFiles(100000),
)
if err != nil {
	return err
}
defer blobFile.Close()

// Or with the lower-level Create API
err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithChangeDetection(blob.ChangeDetectionStrict),
	blob.CreateWithCompression(blob.CompressionZstd),
)

// Archive reading
archive, err := blob.New(indexData, source,
	blob.WithVerifyOnClose(true),         // Always verify (default)
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithPreserveMode(true),
	blob.CopyWithPreserveTimes(true),
)
```

## Monitoring

To understand performance, measure:

1. **Archive creation time**: Total time for `blob.Create()`
2. **Index load time**: Time for `blob.New()`
3. **Single file read latency**: Time for `ReadFile()` including network and decompression
4. **Extraction throughput**: Files per second or MB/s for `CopyDir()`
5. **Memory usage**: Peak RSS during operations

Example timing:

```go
start := time.Now()
err := archive.CopyDir("/dest", ".")
log.Printf("extraction took %v", time.Since(start))
```

## See Also

- [Architecture](../explanation/architecture) - Understand how the format affects performance
- [Creating Archives](creating-archives) - Options that affect read performance
- [Caching](caching) - Improve repeated read performance
- [Block Caching](block-caching) - Block-level caching for remote sources
