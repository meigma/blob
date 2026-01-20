package blob

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Save writes the blob archive to the specified paths.
//
// Uses atomic writes (temp file + rename) to prevent partial writes on failure.
// Parent directories are created as needed.
func (b *Blob) Save(indexPath, dataPath string) error {
	// Create parent directories if needed
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o750); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o750); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	// Write index file atomically
	if err := writeFileAtomic(indexPath, b.indexData); err != nil {
		return fmt.Errorf("write index file: %w", err)
	}

	// Stream data file atomically
	if err := streamFileAtomic(dataPath, b.Stream()); err != nil {
		// Clean up index file on failure
		os.Remove(indexPath)
		return fmt.Errorf("write data file: %w", err)
	}

	return nil
}

// writeFileAtomic writes data to a temp file then renames to target,
// ensuring atomic replacement of the target file.
func writeFileAtomic(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// streamFileAtomic streams from reader to a temp file then renames to target,
// ensuring atomic replacement of the target file.
func streamFileAtomic(target string, r io.Reader) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
