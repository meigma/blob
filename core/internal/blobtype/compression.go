package blobtype

// Compression identifies the compression algorithm used for a file.
type Compression uint8

const (
	CompressionNone Compression = iota
	CompressionZstd
)

// String returns the human-readable name of the compression algorithm.
func (c Compression) String() string {
	switch c {
	case CompressionNone:
		return "none"
	case CompressionZstd:
		return "zstd"
	default:
		return "unknown"
	}
}
