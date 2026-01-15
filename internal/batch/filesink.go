package batch

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileSink writes entries to the filesystem with atomic writes.
//
// Files are written to a temporary file in the same directory,
// then renamed to the final path on Commit. This ensures that
// partially written files are never visible at the final path.
type FileSink struct {
	destDir       string
	overwrite     bool
	preserveMode  bool
	preserveTimes bool
}

// FileSinkOption configures a FileSink.
type FileSinkOption func(*FileSink)

// WithOverwrite allows overwriting existing files.
// By default, existing files are skipped.
func WithOverwrite(overwrite bool) FileSinkOption {
	return func(s *FileSink) {
		s.overwrite = overwrite
	}
}

// WithPreserveMode preserves file permission modes from the archive.
// By default, modes are not preserved (files use umask defaults).
func WithPreserveMode(preserve bool) FileSinkOption {
	return func(s *FileSink) {
		s.preserveMode = preserve
	}
}

// WithPreserveTimes preserves file modification times from the archive.
// By default, times are not preserved (files use current time).
func WithPreserveTimes(preserve bool) FileSinkOption {
	return func(s *FileSink) {
		s.preserveTimes = preserve
	}
}

// NewFileSink creates a FileSink that writes to destDir.
//
// destDir must be an absolute path or relative to the current directory.
// Parent directories are created automatically as needed.
func NewFileSink(destDir string, opts ...FileSinkOption) *FileSink {
	s := &FileSink{
		destDir: destDir,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ShouldProcess returns false if the file already exists and overwrite is disabled.
func (s *FileSink) ShouldProcess(entry *Entry) bool {
	if s.overwrite {
		return true
	}
	destPath := filepath.Join(s.destDir, filepath.FromSlash(entry.Path))
	_, err := os.Stat(destPath)
	return os.IsNotExist(err)
}

// Writer returns a Committer that writes to a temp file and renames on Commit.
func (s *FileSink) Writer(entry *Entry) (Committer, error) {
	destPath := filepath.Join(s.destDir, filepath.FromSlash(entry.Path))

	// Create parent directories
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Create temp file in same directory (for atomic rename)
	tempFile, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	return &fileCommitter{
		entry:    entry,
		destPath: destPath,
		tempFile: tempFile,
		sink:     s,
	}, nil
}

// fileCommitter writes to a temp file and renames on Commit.
type fileCommitter struct {
	entry    *Entry
	destPath string
	tempFile *os.File
	sink     *FileSink
}

// Write implements io.Writer.
func (c *fileCommitter) Write(p []byte) (int, error) {
	return c.tempFile.Write(p)
}

// Commit closes the temp file, applies metadata, and renames to final path.
func (c *fileCommitter) Commit() error {
	tempPath := c.tempFile.Name()

	// Close the temp file
	if err := c.tempFile.Close(); err != nil {
		_ = os.Remove(tempPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("close temp file: %w", err)
	}

	// Apply file mode if requested
	if c.sink.preserveMode {
		if err := os.Chmod(tempPath, c.entry.Mode.Perm()); err != nil {
			_ = os.Remove(tempPath) //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("chmod: %w", err)
		}
	}

	// Apply modification time if requested
	if c.sink.preserveTimes {
		if err := os.Chtimes(tempPath, c.entry.ModTime, c.entry.ModTime); err != nil {
			_ = os.Remove(tempPath) //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("chtimes: %w", err)
		}
	}

	// Atomic rename to final path
	if err := os.Rename(tempPath, c.destPath); err != nil {
		_ = os.Remove(tempPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("rename to %s: %w", c.destPath, err)
	}

	return nil
}

// Discard closes and removes the temp file.
func (c *fileCommitter) Discard() error {
	tempPath := c.tempFile.Name()
	_ = c.tempFile.Close() //nolint:errcheck // we're cleaning up
	return os.Remove(tempPath)
}
