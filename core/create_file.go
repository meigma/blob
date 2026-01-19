package blob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CreateBlob creates a blob archive from srcDir and writes it to destDir.
//
// By default, files are named "index.blob" and "data.blob".
// Use CreateBlobWithIndexName and CreateBlobWithDataName to override.
//
// Returns a BlobFile that must be closed to release file handles.
func CreateBlob(ctx context.Context, srcDir, destDir string, opts ...CreateBlobOption) (*BlobFile, error) {
	cfg := createBlobConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	indexPath := filepath.Join(destDir, cfg.getIndexName())
	dataPath := filepath.Join(destDir, cfg.getDataName())

	// Create destination directory if needed
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return nil, fmt.Errorf("create destination directory: %w", err)
	}

	// Create index file
	indexFile, err := os.Create(indexPath) //nolint:gosec // User-provided path is intentional
	if err != nil {
		return nil, fmt.Errorf("create index file: %w", err)
	}

	// Create data file
	dataFile, err := os.Create(dataPath) //nolint:gosec // User-provided path is intentional
	if err != nil {
		indexFile.Close()
		os.Remove(indexPath)
		return nil, fmt.Errorf("create data file: %w", err)
	}

	// Run Create
	if err := Create(ctx, srcDir, indexFile, dataFile, cfg.createOpts...); err != nil {
		indexFile.Close()
		dataFile.Close()
		os.Remove(indexPath)
		os.Remove(dataPath)
		return nil, fmt.Errorf("create archive: %w", err)
	}

	// Close write handles
	if err := indexFile.Close(); err != nil {
		dataFile.Close()
		os.Remove(indexPath)
		os.Remove(dataPath)
		return nil, fmt.Errorf("close index file: %w", err)
	}
	if err := dataFile.Close(); err != nil {
		os.Remove(indexPath)
		os.Remove(dataPath)
		return nil, fmt.Errorf("close data file: %w", err)
	}

	// Reopen for reading via OpenFile
	return OpenFile(indexPath, dataPath)
}
