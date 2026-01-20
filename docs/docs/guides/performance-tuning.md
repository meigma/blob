---
sidebar_position: 7
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

## Pull Options (OCI Client)

When pulling from OCI registries, configure decoder options via `PullWith*` options:

### Maximum File Size

To allow larger individual files:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithMaxFileSize(512 << 20), // 512 MB limit
)
```

The default is 256 MB. Set to 0 to disable the limit entirely (not recommended for untrusted archives).

### Decoder Concurrency

To control zstd decoder parallelism:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithDecoderConcurrency(2), // 2 goroutines per decoder
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
archive, err := c.Pull(ctx, ref,
	blob.PullWithDecoderLowmem(true),
)
```

This mode uses approximately 3x less memory but decompresses about 2x slower.

### Skip Verification on Close

By default, closing a file without reading to EOF drains remaining data to verify the hash. To skip this:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithVerifyOnClose(false),
)
```

When disabled:
- `Close()` returns immediately without reading remaining data
- Integrity is only guaranteed when callers read to EOF
- Use when you intentionally read partial files

**Warning**: Partial reads may return unverified data. Only disable if your use case does not require full integrity verification.

### Index Size Limits

Limit the maximum index size to prevent memory exhaustion:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithMaxIndexSize(16 << 20), // 16 MB limit
)
```

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

## Cache Tuning

### Quick Setup

For most use cases, `WithCacheDir` provides good defaults:

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/var/cache/blob"),
)
```

This creates all cache layers with sensible default sizes.

### RefCache TTL

For mutable tags like `latest` that change frequently:

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/var/cache/blob"),
	blob.WithRefCacheTTL(1 * time.Minute),  // Refresh every minute
)
```

For stable environments:

```go
blob.WithRefCacheTTL(1 * time.Hour)  // Refresh hourly
```

For immutable references (digests only):

```go
blob.WithRefCacheTTL(0)  // Never expire
```

### Individual Cache Configuration

For fine-grained control, configure caches individually:

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithContentCacheDir("/fast-ssd/content"),
	blob.WithBlockCacheDir("/fast-ssd/blocks"),
	blob.WithRefCacheDir("/var/cache/refs"),
	blob.WithManifestCacheDir("/var/cache/manifests"),
	blob.WithIndexCacheDir("/var/cache/indexes"),
)
```

See [Caching](caching) for advanced cache configuration including custom sizes and implementations.

## Tuning Scenarios

### Memory-Constrained Environment

For systems with limited RAM (< 512 MB available):

```go
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/var/cache/blob"),
)

archive, err := c.Pull(ctx, ref,
	blob.PullWithDecoderLowmem(true),         // Low memory decompression
	blob.PullWithDecoderConcurrency(1),       // Single-threaded
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithWorkers(2),               // Few parallel writers
	blob.CopyWithReadConcurrency(2),       // Few parallel reads
	blob.CopyWithReadAheadBytes(16 << 20), // 16 MB read budget
)
```

### High-Latency OCI Registry

For archives on slow networks or distant registries (> 200ms RTT):

```go
import (
	"time"

	"github.com/meigma/blob"
)

// Aggressive caching to minimize round trips
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/var/cache/blob"),
	blob.WithRefCacheTTL(30 * time.Minute),
)

archive, err := c.Pull(ctx, ref,
	blob.PullWithDecoderConcurrency(0),  // Max decoder parallelism
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithWorkers(4),           // Parallel file writing
	blob.CopyWithReadConcurrency(16),  // Many concurrent requests
)
```

### Maximum Throughput

For fastest possible extraction on capable hardware:

```go
archive, err := c.Pull(ctx, ref,
	blob.PullWithDecoderConcurrency(0),  // Use all cores
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
import "github.com/meigma/blob"

// Push with strict change detection
c, _ := blob.NewClient(
	blob.WithDockerConfig(),
	blob.WithCacheDir("/tmp/blob-cache"),
)

err := c.Push(ctx, ref, srcDir,
	blob.PushWithCompression(blob.CompressionZstd),
	blob.PushWithSkipCompression(blob.DefaultSkipCompression(1024)),
	blob.PushWithChangeDetection(blob.ChangeDetectionStrict),
	blob.PushWithMaxFiles(100000),
)

// Pull with full verification
archive, err := c.Pull(ctx, ref,
	blob.PullWithVerifyOnClose(true),  // Always verify (default)
)

err = archive.CopyDir("/dest", ".",
	blob.CopyWithPreserveMode(true),
	blob.CopyWithPreserveTimes(true),
)
```

## Monitoring

To understand performance, measure:

1. **Push time**: Total time for `c.Push()`
2. **Pull time**: Time for `c.Pull()` (index download + setup)
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

- [OCI Client](oci-client) - Push and pull archives
- [Caching](caching) - Cache configuration and sizing
- [Architecture](../explanation/architecture) - Understand how the format affects performance
- [Creating Archives](creating-archives) - Options that affect read performance
- [Advanced Usage](advanced) - Low-level options for custom cache implementations
