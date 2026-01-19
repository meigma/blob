package file

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// mockByteSource implements ByteSource for testing.
type mockByteSource struct {
	data     []byte
	sourceID string
}

func (m *mockByteSource) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(m.data)) {
		return 0, nil
	}
	n = copy(p, m.data[off:])
	return n, nil
}

func (m *mockByteSource) Size() int64 {
	return int64(len(m.data))
}

func (m *mockByteSource) SourceID() string {
	return m.sourceID
}

func newMockSource(data []byte) ByteSource {
	sum := sha256.Sum256(data)
	return &mockByteSource{
		data:     data,
		sourceID: "mock:" + hex.EncodeToString(sum[:]),
	}
}

func compress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}
	if _, err := enc.Write(data); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("failed to close encoder: %v", err)
	}
	return buf.Bytes()
}

func hashOf(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func TestOps_ReadAll(t *testing.T) {
	t.Parallel()

	content := []byte("hello world, this is test content")
	compressed := compress(t, content)

	tests := []struct {
		name        string
		sourceData  []byte
		entry       *Entry
		want        []byte
		wantErr     error
		errContains string
	}{
		{
			name:       "uncompressed file",
			sourceData: content,
			entry: &Entry{
				Path:         "test.txt",
				DataOffset:   0,
				DataSize:     uint64(len(content)),
				OriginalSize: uint64(len(content)),
				Hash:         hashOf(content),
				Compression:  CompressionNone,
			},
			want: content,
		},
		{
			name:       "compressed file",
			sourceData: compressed,
			entry: &Entry{
				Path:         "test.txt",
				DataOffset:   0,
				DataSize:     uint64(len(compressed)),
				OriginalSize: uint64(len(content)),
				Hash:         hashOf(content),
				Compression:  CompressionZstd,
			},
			want: content,
		},
		{
			name:       "file with offset",
			sourceData: append([]byte("prefix"), content...),
			entry: &Entry{
				Path:         "test.txt",
				DataOffset:   6, // skip "prefix"
				DataSize:     uint64(len(content)),
				OriginalSize: uint64(len(content)),
				Hash:         hashOf(content),
				Compression:  CompressionNone,
			},
			want: content,
		},
		{
			name:       "hash mismatch",
			sourceData: content,
			entry: &Entry{
				Path:         "test.txt",
				DataOffset:   0,
				DataSize:     uint64(len(content)),
				OriginalSize: uint64(len(content)),
				Hash:         make([]byte, sha256.Size), // wrong hash
				Compression:  CompressionNone,
			},
			wantErr: ErrHashMismatch,
		},
		{
			name:       "invalid hash length",
			sourceData: content,
			entry: &Entry{
				Path:         "test.txt",
				DataOffset:   0,
				DataSize:     uint64(len(content)),
				OriginalSize: uint64(len(content)),
				Hash:         []byte("short"),
				Compression:  CompressionNone,
			},
			errContains: "invalid hash length",
		},
		{
			name:       "data extends beyond source",
			sourceData: content[:10],
			entry: &Entry{
				Path:         "test.txt",
				DataOffset:   0,
				DataSize:     uint64(len(content)),
				OriginalSize: uint64(len(content)),
				Hash:         hashOf(content),
				Compression:  CompressionNone,
			},
			wantErr: ErrSizeOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := newMockSource(tt.sourceData)
			ops := NewReader(source, WithMaxFileSize(0)) // no limit

			got, err := ops.ReadAll(tt.entry)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ReadAll() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if tt.errContains != "" {
				if err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.errContains)) {
					t.Errorf("ReadAll() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadAll() unexpected error: %v", err)
			}

			if !bytes.Equal(got, tt.want) {
				t.Errorf("ReadAll() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOps_Options(t *testing.T) {
	t.Parallel()

	content := []byte("test content")
	source := newMockSource(content)

	t.Run("default options", func(t *testing.T) {
		t.Parallel()
		ops := NewReader(source)
		if ops.MaxFileSize() != DefaultMaxFileSize {
			t.Errorf("MaxFileSize() = %d, want %d", ops.MaxFileSize(), DefaultMaxFileSize)
		}
	})

	t.Run("custom max file size", func(t *testing.T) {
		t.Parallel()
		ops := NewReader(source, WithMaxFileSize(1000))
		if ops.MaxFileSize() != 1000 {
			t.Errorf("MaxFileSize() = %d, want %d", ops.MaxFileSize(), 1000)
		}
	})

	t.Run("file exceeds max size", func(t *testing.T) {
		t.Parallel()
		ops := NewReader(source, WithMaxFileSize(5)) // very small limit

		entry := &Entry{
			Path:         "test.txt",
			DataOffset:   0,
			DataSize:     uint64(len(content)),
			OriginalSize: uint64(len(content)),
			Hash:         hashOf(content),
			Compression:  CompressionNone,
		}

		_, err := ops.ReadAll(entry)
		if !errors.Is(err, ErrSizeOverflow) {
			t.Errorf("ReadAll() error = %v, want ErrSizeOverflow", err)
		}
	})
}

func TestOps_EmptyFile(t *testing.T) {
	t.Parallel()

	empty := []byte{}
	source := newMockSource(empty)
	ops := NewReader(source, WithMaxFileSize(0))

	entry := &Entry{
		Path:         "empty.txt",
		DataOffset:   0,
		DataSize:     0,
		OriginalSize: 0,
		Hash:         hashOf(empty),
		Compression:  CompressionNone,
	}

	got, err := ops.ReadAll(entry)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if len(got) != 0 {
		t.Errorf("ReadAll() = %q, want empty", got)
	}
}
