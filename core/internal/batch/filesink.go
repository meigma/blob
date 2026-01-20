package batch

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileSink writes entries to the filesystem.
//
// By default, files are written to a temporary file in the same directory
// and renamed to the final path on Commit. This ensures that partially
// written files are never visible at the final path.
type FileSink struct {
	destDir       string
	overwrite     bool
	preserveMode  bool
	preserveTimes bool
	directWrite   bool
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

// WithDirectWrites disables temp files and writes directly to the final path.
func WithDirectWrites(enabled bool) FileSinkOption {
	return func(s *FileSink) {
		s.directWrite = enabled
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
	if !fs.ValidPath(entry.Path) {
		return false
	}
	destPath := filepath.Join(s.destDir, filepath.FromSlash(entry.Path))
	_, err := os.Stat(destPath)
	return os.IsNotExist(err)
}

// Writer returns a Committer that writes to a temp file and renames on Commit.
func (s *FileSink) Writer(entry *Entry) (Committer, error) {
	if !fs.ValidPath(entry.Path) {
		return nil, &fs.PathError{Op: "copy", Path: entry.Path, Err: fs.ErrInvalid}
	}
	destPath := filepath.Join(s.destDir, filepath.FromSlash(entry.Path))
	destRel := filepath.FromSlash(entry.Path)

	// Create parent directories
	dir := filepath.Dir(destPath)
	root, err := os.OpenRoot(s.destDir)
	if err != nil {
		return nil, fmt.Errorf("open destination root %s: %w", s.destDir, err)
	}
	if err := root.MkdirAll(filepath.Dir(destRel), 0o750); err != nil {
		_ = root.Close() //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("create directory %s: %w", dir, err)
	}

	if s.directWrite {
		file, err := root.OpenFile(destRel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			_ = root.Close() //nolint:errcheck // best-effort cleanup
			return nil, fmt.Errorf("create file %s: %w", destPath, err)
		}
		return &directCommitter{
			entry:    entry,
			destPath: destPath,
			destRel:  destRel,
			file:     file,
			root:     root,
			sink:     s,
		}, nil
	}

	// Create temp file in same directory (for atomic rename)
	tempFile, tempRel, err := createTempFile(root, filepath.Dir(destRel), ".blob-")
	if err != nil {
		_ = root.Close() //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	return &fileCommitter{
		entry:    entry,
		destPath: destPath,
		destRel:  destRel,
		tempFile: tempFile,
		tempRel:  tempRel,
		root:     root,
		sink:     s,
	}, nil
}

// fileCommitter writes to a temp file and renames on Commit.
type fileCommitter struct {
	entry    *Entry
	destPath string
	destRel  string
	tempFile *os.File
	tempRel  string
	root     *os.Root
	sink     *FileSink
}

// Write implements io.Writer.
func (c *fileCommitter) Write(p []byte) (int, error) {
	return c.tempFile.Write(p)
}

// Commit closes the temp file, applies metadata, and renames to final path.
func (c *fileCommitter) Commit() error {
	// Close the temp file
	if err := c.tempFile.Close(); err != nil {
		_ = c.root.Remove(c.tempRel) //nolint:errcheck // best-effort cleanup
		_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("close temp file: %w", err)
	}

	// Apply file mode if requested
	if c.sink.preserveMode {
		if err := c.root.Chmod(c.tempRel, c.entry.Mode.Perm()); err != nil {
			_ = c.root.Remove(c.tempRel) //nolint:errcheck // best-effort cleanup
			_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("chmod: %w", err)
		}
	}

	// Apply modification time if requested
	if c.sink.preserveTimes {
		if err := c.root.Chtimes(c.tempRel, c.entry.ModTime, c.entry.ModTime); err != nil {
			_ = c.root.Remove(c.tempRel) //nolint:errcheck // best-effort cleanup
			_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("chtimes: %w", err)
		}
	}

	// Atomic rename to final path
	if err := c.root.Rename(c.tempRel, c.destRel); err != nil {
		_ = c.root.Remove(c.tempRel) //nolint:errcheck // best-effort cleanup
		_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("rename to %s: %w", c.destPath, err)
	}

	_ = c.root.Close() //nolint:errcheck // best-effort cleanup
	return nil
}

// Discard closes and removes the temp file.
func (c *fileCommitter) Discard() error {
	_ = c.tempFile.Close() //nolint:errcheck // we're cleaning up
	if err := c.root.Remove(c.tempRel); err != nil {
		_ = c.root.Close() //nolint:errcheck // best-effort cleanup
		return err
	}
	return c.root.Close()
}

// directCommitter writes directly to the final path.
type directCommitter struct {
	entry    *Entry
	destPath string
	destRel  string
	file     *os.File
	root     *os.Root
	sink     *FileSink
}

// Write implements io.Writer.
func (c *directCommitter) Write(p []byte) (int, error) {
	return c.file.Write(p)
}

// Commit closes the file and applies metadata.
func (c *directCommitter) Commit() error {
	if err := c.file.Close(); err != nil {
		_ = c.root.Remove(c.destRel) //nolint:errcheck // best-effort cleanup
		_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("close file: %w", err)
	}

	if c.sink.preserveMode {
		if err := c.root.Chmod(c.destRel, c.entry.Mode.Perm()); err != nil {
			_ = c.root.Remove(c.destRel) //nolint:errcheck // best-effort cleanup
			_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("chmod: %w", err)
		}
	}

	if c.sink.preserveTimes {
		if err := c.root.Chtimes(c.destRel, c.entry.ModTime, c.entry.ModTime); err != nil {
			_ = c.root.Remove(c.destRel) //nolint:errcheck // best-effort cleanup
			_ = c.root.Close()           //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("chtimes: %w", err)
		}
	}

	_ = c.root.Close() //nolint:errcheck // best-effort cleanup
	return nil
}

// Discard closes and removes the file.
func (c *directCommitter) Discard() error {
	_ = c.file.Close() //nolint:errcheck // best-effort cleanup
	if err := c.root.Remove(c.destRel); err != nil {
		_ = c.root.Close() //nolint:errcheck // best-effort cleanup
		return err
	}
	return c.root.Close()
}

func createTempFile(root *os.Root, dir, prefix string) (*os.File, string, error) {
	const attempts = 10
	for range attempts {
		name, err := randomSuffix()
		if err != nil {
			return nil, "", err
		}
		relPath := filepath.Join(dir, prefix+name)
		f, err := root.OpenFile(relPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return f, relPath, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("create temp file: exhausted retries")
}

func randomSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
