---
sidebar_position: 1
---

# Creating Archives

How to build blob archives from directories for storage in OCI registries.

## Basic Usage

To create an archive, provide a source directory and writers for the index and data:

```go
import (
	"context"
	"os"

	"github.com/meigma/blob"
)

func createArchive(srcDir string) error {
	indexFile, err := os.Create("archive.index")
	if err != nil {
		return err
	}
	defer indexFile.Close()

	dataFile, err := os.Create("archive.data")
	if err != nil {
		return err
	}
	defer dataFile.Close()

	return blob.Create(context.Background(), srcDir, indexFile, dataFile)
}
```

The function walks the source directory recursively, writing file contents to the data writer and metadata to the index writer. Files are written in path-sorted order to enable efficient directory fetches.

## Compression

To enable zstd compression, use `CreateWithCompression`:

```go
err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithCompression(blob.CompressionZstd),
)
```

Compression reduces data size but requires decompression when reading. For typical source code and configuration files, expect 2-4x compression ratios.

Available compression options:
- `blob.CompressionNone` - Store files uncompressed (default)
- `blob.CompressionZstd` - Use zstd compression

## Skipping Compression

Some files compress poorly because they are already compressed (images, videos, archives) or too small to benefit. Use `CreateWithSkipCompression` to skip these:

```go
err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithCompression(blob.CompressionZstd),
	blob.CreateWithSkipCompression(blob.DefaultSkipCompression(1024)),
)
```

`DefaultSkipCompression(minSize)` creates a predicate that skips:
- Files smaller than `minSize` bytes
- Files with known compressed extensions (`.jpg`, `.png`, `.zip`, `.gz`, etc.)

### Custom Skip Predicates

To define custom skip logic, pass additional predicates:

```go
// Skip lock files and generated code
skipGenerated := func(path string, info fs.FileInfo) bool {
	return strings.HasSuffix(path, ".lock") ||
		strings.Contains(path, "/generated/")
}

err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithCompression(blob.CompressionZstd),
	blob.CreateWithSkipCompression(
		blob.DefaultSkipCompression(1024),
		skipGenerated,
	),
)
```

If any predicate returns true, the file is stored uncompressed.

## Change Detection

For build pipelines, enable strict change detection to catch files that change during archive creation:

```go
err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithChangeDetection(blob.ChangeDetectionStrict),
)
```

With strict change detection, Create verifies that file size and modification time remain unchanged after reading. If a file changes mid-write, Create returns an error rather than producing an archive with inconsistent content.

Change detection modes:
- `blob.ChangeDetectionNone` - No verification (default, fewer syscalls)
- `blob.ChangeDetectionStrict` - Verify files did not change during creation

## File Limits

To protect against runaway archive creation, limit the number of files:

```go
// Allow up to 50,000 files
err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithMaxFiles(50000),
)
```

If the source directory contains more files than the limit, Create returns `blob.ErrTooManyFiles`.

Special values:
- `0` - Use default limit (200,000 files)
- Negative values - No limit

## Memory Considerations

Create builds the entire index in memory before writing. Memory usage scales with the number of files and average path length.

Rough guide:
- 10,000 files: ~3-5 MB
- 100,000 files: ~30-50 MB
- 200,000 files: ~60-100 MB

For archives approaching the default 200,000 file limit, ensure the build environment has sufficient memory (256 MB+ recommended).

## Cancellation

Pass a context to support cancellation of long-running archive creation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

err := blob.Create(ctx, srcDir, indexW, dataW,
	blob.CreateWithCompression(blob.CompressionZstd),
)
if errors.Is(err, context.DeadlineExceeded) {
	// Archive creation timed out
}
```

## Complete Example

A production archive creation function with all options:

```go
func createProductionArchive(srcDir, indexPath, dataPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	indexFile, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("create index file: %w", err)
	}
	defer indexFile.Close()

	dataFile, err := os.Create(dataPath)
	if err != nil {
		return fmt.Errorf("create data file: %w", err)
	}
	defer dataFile.Close()

	err = blob.Create(ctx, srcDir, indexFile, dataFile,
		blob.CreateWithCompression(blob.CompressionZstd),
		blob.CreateWithSkipCompression(blob.DefaultSkipCompression(1024)),
		blob.CreateWithChangeDetection(blob.ChangeDetectionStrict),
		blob.CreateWithMaxFiles(100000),
	)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	return nil
}
```

## See Also

- [Architecture](../explanation/architecture) - How the archive format works
- [Integrity](../explanation/integrity) - How content verification works
