---
sidebar_position: 6
---

# Extracting Files

How to extract archive contents to the local filesystem.

## Extracting from OCI Registry

Pull an archive from an OCI registry and extract its contents:

```go
import "github.com/meigma/blob"

func extractFromRegistry(ref, destDir string) error {
	c, err := blob.NewClient(blob.WithDockerConfig())
	if err != nil {
		return err
	}

	// Pull the archive
	archive, err := c.Pull(ctx, ref)
	if err != nil {
		return err
	}

	// Extract all files
	_, err = archive.CopyDir(destDir, ".")
	return err
}
```

The pulled archive fetches file data lazily. For extraction, data is streamed via range requests as needed.

### CLI: Reading and Extracting Files

```bash
# Print file to stdout
blob cat ghcr.io/myorg/archive:v1 config.json

# Copy specific files
blob cp ghcr.io/myorg/archive:v1:config.json ./local/
blob cp ghcr.io/myorg/archive:v1:src/main.go ./local/

# Pull entire archive
blob pull ghcr.io/myorg/archive:v1 ./dest

# Pull with options
blob pull --overwrite --preserve-mode ghcr.io/myorg/archive:v1 ./dest

# Extract specific prefix
blob pull --prefix=src ghcr.io/myorg/archive:v1 ./dest
```

## Working with BlobFile

When using `OpenFile` or `CreateBlob` from the core package, you receive a `*BlobFile` which embeds `*Blob`. All extraction methods work identically:

```go
import blobcore "github.com/meigma/blob/core"

blobFile, err := blobcore.OpenFile("index.blob", "data.blob")
if err != nil {
	return err
}
defer blobFile.Close()

// All Blob methods work on BlobFile
_, err = blobFile.CopyDir("/dest", ".")
```

The examples below use `*Blob` but apply equally to `*BlobFile`.

## Extracting Specific Files

To extract specific files by path, use `CopyTo`:

```go
_, err := archive.CopyTo("/dest/dir", "config.json", "lib/utils.go", "main.go")
```

The files are extracted to the destination directory, preserving their relative paths:
- `config.json` -> `/dest/dir/config.json`
- `lib/utils.go` -> `/dest/dir/lib/utils.go`

Parent directories are created automatically.

To pass options, use `CopyToWithOptions`:

```go
paths := []string{"config.json", "lib/utils.go"}
_, err := archive.CopyToWithOptions("/dest/dir", paths,
	blob.CopyWithOverwrite(true),
	blob.CopyWithPreserveMode(true),
)
```

## Extracting Directories

To extract all files under a directory prefix, use `CopyDir`:

```go
// Extract everything under src/
stats, err := archive.CopyDir("/dest/dir", "src")

// Extract the entire archive
stats, err = archive.CopyDir("/dest/dir", ".")

// stats.FileCount contains the number of files extracted
// stats.TotalBytes contains the total uncompressed size
// stats.Skipped contains the number of files skipped (already exist)
```

Files matching the prefix are extracted to the destination directory with their full archive paths:
- Archive path `src/main.go` with prefix `src` -> `/dest/dir/src/main.go`
- Archive path `lib/utils.go` with prefix `.` -> `/dest/dir/lib/utils.go`

## Overwrite Behavior

By default, existing files are skipped. To overwrite:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithOverwrite(true),
)
```

This is useful when you want to ensure files match the archive contents, even if the destination already exists.

## Preserving Metadata

### File Modes

To preserve file permission modes from the archive:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithPreserveMode(true),
)
```

Without this option, extracted files use the current umask defaults.

### Modification Times

To preserve file modification times:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithPreserveTimes(true),
)
```

### Both Mode and Times

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithPreserveMode(true),
	blob.CopyWithPreserveTimes(true),
)
```

## Clean Destination Mode

For faster extraction when starting fresh, use `CopyWithCleanDest`:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithCleanDest(true),
)
```

This option:
1. Removes the destination directory (or subdirectory if prefix is specified)
2. Writes files directly to their final paths (no temp files)
3. Automatically enables overwrite mode

**Warning**: This deletes existing content in the destination. Use with caution.

`CopyWithCleanDest` is only supported by `CopyDir`, not `CopyTo`.

### Safety Checks

The clean destination option includes safety checks:
- Refuses to delete filesystem roots (`/`, `C:\`)
- Refuses to delete the volume root on Windows
- Requires a non-empty destination path

## Parallel Extraction

### Worker Count

To control the number of parallel file writers:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithWorkers(8), // Use 8 parallel workers
)
```

Values:
- `0` - Use automatic heuristics (default)
- Negative - Force serial processing
- Positive - Use exactly N workers

### Read Concurrency

For remote archives, control the number of concurrent range reads:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithReadConcurrency(8), // 8 concurrent HTTP requests
)
```

The default is 4 concurrent reads. Higher values reduce latency but increase memory usage and server load.

### Read-Ahead Budget

To limit memory usage during parallel reads:

```go
_, err := archive.CopyDir("/dest/dir", ".",
	blob.CopyWithReadAheadBytes(64 * 1024 * 1024), // 64MB budget
)
```

This caps the total size of buffered read-ahead data. Use this when extracting large files to prevent memory exhaustion.

## Error Handling

Extraction errors include the file path and underlying cause:

```go
_, err := archive.CopyDir("/dest/dir", ".")
if err != nil {
	// Errors are wrapped with path context
	log.Printf("extraction failed: %v", err)
}
```

Common error scenarios:
- Destination directory not writable
- Disk full
- Hash mismatch (corrupted archive data)
- Network error (for remote archives)

## Complete Example

A production extraction function with all options:

```go
func extractArchive(archive *blob.Blob, destDir string, opts ExtractOptions) error {
	copyOpts := []blob.CopyOption{
		blob.CopyWithOverwrite(opts.Overwrite),
	}

	if opts.PreserveMetadata {
		copyOpts = append(copyOpts,
			blob.CopyWithPreserveMode(true),
			blob.CopyWithPreserveTimes(true),
		)
	}

	if opts.Clean {
		copyOpts = append(copyOpts, blob.CopyWithCleanDest(true))
	}

	if opts.Workers > 0 {
		copyOpts = append(copyOpts, blob.CopyWithWorkers(opts.Workers))
	}

	if opts.ReadConcurrency > 0 {
		copyOpts = append(copyOpts, blob.CopyWithReadConcurrency(opts.ReadConcurrency))
	}

	prefix := opts.Prefix
	if prefix == "" {
		prefix = "."
	}

	_, err = archive.CopyDir(destDir, prefix, copyOpts...)
	return err
}

type ExtractOptions struct {
	Prefix           string
	Overwrite        bool
	PreserveMetadata bool
	Clean            bool
	Workers          int
	ReadConcurrency  int
}
```

Usage:

```go
err := extractArchive(archive, "/app/deploy", ExtractOptions{
	Prefix:           "dist",
	Overwrite:        true,
	PreserveMetadata: true,
	Clean:            true,
	Workers:          4,
	ReadConcurrency:  8,
})
```

## See Also

- [CLI Reference](../reference/cli) - Command-line extraction commands (`pull`, `cp`, `cat`)
- [OCI Client](oci-client) - Pull archives from registries
- [Performance Tuning](performance-tuning) - Advanced extraction optimization
- [Caching](caching) - Cache content for faster repeated extraction
