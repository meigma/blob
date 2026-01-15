package blob

import (
	"io/fs"

	"github.com/meigma/blob/internal/batch"
	"github.com/meigma/blob/internal/pathutil"
)

// CopyOption configures CopyTo and CopyDir operations.
type CopyOption func(*copyConfig)

type copyConfig struct {
	overwrite     bool
	preserveMode  bool
	preserveTimes bool
	workers       int
}

// CopyWithOverwrite allows overwriting existing files.
// By default, existing files are skipped.
func CopyWithOverwrite(overwrite bool) CopyOption {
	return func(c *copyConfig) {
		c.overwrite = overwrite
	}
}

// CopyWithPreserveMode preserves file permission modes from the archive.
// By default, modes are not preserved (files use umask defaults).
func CopyWithPreserveMode(preserve bool) CopyOption {
	return func(c *copyConfig) {
		c.preserveMode = preserve
	}
}

// CopyWithPreserveTimes preserves file modification times from the archive.
// By default, times are not preserved (files use current time).
func CopyWithPreserveTimes(preserve bool) CopyOption {
	return func(c *copyConfig) {
		c.preserveTimes = preserve
	}
}

// CopyWithWorkers sets the number of workers for parallel processing.
// Values < 0 force serial processing. Zero uses automatic heuristics.
// Values > 0 force a specific worker count.
func CopyWithWorkers(n int) CopyOption {
	return func(c *copyConfig) {
		c.workers = n
	}
}

// CopyTo extracts specific files to a destination directory.
//
// Files are written atomically using temp files and renames.
// Parent directories are created as needed.
//
// By default:
//   - Existing files are skipped (use CopyWithOverwrite to overwrite)
//   - File modes and times are not preserved (use CopyWithPreserveMode/Times)
func (r *Reader) CopyTo(destDir string, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}

	cfg := copyConfig{}
	return r.copyEntries(destDir, r.collectPathEntries(paths), &cfg)
}

// CopyToWithOptions extracts specific files with options.
func (r *Reader) CopyToWithOptions(destDir string, paths []string, opts ...CopyOption) error {
	if len(paths) == 0 {
		return nil
	}

	cfg := copyConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return r.copyEntries(destDir, r.collectPathEntries(paths), &cfg)
}

// CopyDir extracts all files under a directory prefix to a destination.
//
// If prefix is "" or ".", all files in the archive are extracted.
//
// Files are written atomically using temp files and renames.
// Parent directories are created as needed.
//
// By default:
//   - Existing files are skipped (use CopyWithOverwrite to overwrite)
//   - File modes and times are not preserved (use CopyWithPreserveMode/Times)
func (r *Reader) CopyDir(destDir, prefix string, opts ...CopyOption) error {
	cfg := copyConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return r.copyEntries(destDir, r.collectPrefixEntries(prefix), &cfg)
}

// collectPathEntries collects entries for specific paths.
func (r *Reader) collectPathEntries(paths []string) []*batch.Entry {
	entries := make([]*batch.Entry, 0, len(paths))
	for _, path := range paths {
		if !fs.ValidPath(path) {
			continue
		}
		view, ok := r.index.LookupView(path)
		if !ok {
			continue
		}
		entry := entryFromViewWithPath(view, path)
		entries = append(entries, &entry)
	}
	return entries
}

// collectPrefixEntries collects all entries under a prefix.
func (r *Reader) collectPrefixEntries(prefix string) []*batch.Entry {
	if prefix != "" && prefix != "." && !fs.ValidPath(prefix) {
		return nil
	}

	var dirPrefix string
	if prefix == "" || prefix == "." {
		dirPrefix = ""
	} else {
		dirPrefix = pathutil.DirPrefix(prefix)
	}

	var entries []*batch.Entry //nolint:prealloc // size unknown until iteration
	for view := range r.index.EntriesWithPrefixView(dirPrefix) {
		entry := entryFromViewWithPath(view, view.Path())
		entries = append(entries, &entry)
	}
	return entries
}

// copyEntries uses the batch processor to copy entries to destDir.
func (r *Reader) copyEntries(destDir string, entries []*batch.Entry, cfg *copyConfig) error {
	if len(entries) == 0 {
		return nil
	}

	// Create file sink with options
	sinkOpts := []batch.FileSinkOption{
		batch.WithOverwrite(cfg.overwrite),
		batch.WithPreserveMode(cfg.preserveMode),
		batch.WithPreserveTimes(cfg.preserveTimes),
	}
	sink := batch.NewFileSink(destDir, sinkOpts...)

	// Create processor with options
	var procOpts []batch.ProcessorOption
	if cfg.workers != 0 {
		procOpts = append(procOpts, batch.WithWorkers(cfg.workers))
	}
	proc := batch.NewProcessor(r.ops.Source(), r.ops.Pool(), r.maxFileSize, procOpts...)

	return proc.Process(entries, sink)
}
