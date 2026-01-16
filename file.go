package blob

import (
	"fmt"
	"os"
)

// Default file names for blob archives.
const (
	DefaultIndexName = "index.blob"
	DefaultDataName  = "data.blob"
)

// fileSource wraps *os.File to implement ByteSource.
// os.File has ReadAt but not Size, so we cache the size at construction.
type fileSource struct {
	file *os.File
	size int64
}

// newFileSource creates a fileSource from an open file.
func newFileSource(f *os.File) (*fileSource, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat data file: %w", err)
	}
	return &fileSource{file: f, size: info.Size()}, nil
}

// ReadAt implements io.ReaderAt.
func (fs *fileSource) ReadAt(p []byte, off int64) (int, error) {
	return fs.file.ReadAt(p, off)
}

// Size returns the total size of the file.
func (fs *fileSource) Size() int64 {
	return fs.size
}

// BlobFile wraps a Blob with its underlying data file handle.
// Close must be called to release file resources.
//
//nolint:revive // BlobFile is intentionally named for clarity when imported
type BlobFile struct {
	*Blob
	dataFile *os.File
}

// Close closes the underlying data file.
func (bf *BlobFile) Close() error {
	if bf.dataFile == nil {
		return nil
	}
	err := bf.dataFile.Close()
	bf.dataFile = nil
	return err
}

// OpenFile opens a blob archive from index and data files.
//
// The index file is read into memory; the data file is opened for random access.
// The returned BlobFile must be closed to release file resources.
func OpenFile(indexPath, dataPath string, opts ...Option) (*BlobFile, error) {
	// Read index file into memory
	indexData, err := os.ReadFile(indexPath) //nolint:gosec // User-provided path is intentional
	if err != nil {
		return nil, fmt.Errorf("read index file: %w", err)
	}

	// Open data file for random access
	dataFile, err := os.Open(dataPath) //nolint:gosec // User-provided path is intentional
	if err != nil {
		return nil, fmt.Errorf("open data file: %w", err)
	}

	// Wrap data file as ByteSource
	source, err := newFileSource(dataFile)
	if err != nil {
		dataFile.Close()
		return nil, err
	}

	// Create Blob
	b, err := New(indexData, source, opts...)
	if err != nil {
		dataFile.Close()
		return nil, fmt.Errorf("create blob: %w", err)
	}

	return &BlobFile{
		Blob:     b,
		dataFile: dataFile,
	}, nil
}

// Interface compliance for fileSource.
var _ ByteSource = (*fileSource)(nil)

// Ensure BlobFile embeds Blob correctly by verifying interface compliance.
var (
	_ interface{ Close() error } = (*BlobFile)(nil)
)
