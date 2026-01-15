package file

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"testing"
)

func TestFile_Read(t *testing.T) {
	t.Parallel()

	content := []byte("hello world, this is test content for streaming reads")
	compressed := compress(t, content)

	tests := []struct {
		name       string
		sourceData []byte
		entry      *Entry
		want       []byte
		wantErr    error
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := newMockSource(tt.sourceData)
			ops := NewReader(source, WithMaxFileSize(0))
			f := ops.OpenFile(tt.entry, true)
			defer f.Close()

			got, err := io.ReadAll(f)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Read() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}

			if !bytes.Equal(got, tt.want) {
				t.Errorf("Read() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFile_PartialRead(t *testing.T) {
	t.Parallel()

	content := []byte("hello world, this is test content")
	source := newMockSource(content)
	ops := NewReader(source, WithMaxFileSize(0))

	entry := &Entry{
		Path:         "test.txt",
		DataOffset:   0,
		DataSize:     uint64(len(content)),
		OriginalSize: uint64(len(content)),
		Hash:         hashOf(content),
		Compression:  CompressionNone,
	}

	f := ops.OpenFile(entry, false) // don't verify on close
	defer f.Close()

	// Read only first 5 bytes
	buf := make([]byte, 5)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Read() n = %d, want 5", n)
	}
	if !bytes.Equal(buf, content[:5]) {
		t.Errorf("Read() = %q, want %q", buf, content[:5])
	}

	// Close without reading rest - should not error since verifyOnClose=false
	err = f.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestFile_VerifyOnClose(t *testing.T) {
	t.Parallel()

	content := []byte("hello world")
	source := newMockSource(content)
	ops := NewReader(source, WithMaxFileSize(0))

	t.Run("verifies on close when enabled", func(t *testing.T) {
		t.Parallel()

		entry := &Entry{
			Path:         "test.txt",
			DataOffset:   0,
			DataSize:     uint64(len(content)),
			OriginalSize: uint64(len(content)),
			Hash:         hashOf(content),
			Compression:  CompressionNone,
		}

		f := ops.OpenFile(entry, true)

		// Read partial, then close - should drain and verify
		buf := make([]byte, 5)
		_, err := f.Read(buf)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}

		err = f.Close()
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("catches hash mismatch on close", func(t *testing.T) {
		t.Parallel()

		entry := &Entry{
			Path:         "test.txt",
			DataOffset:   0,
			DataSize:     uint64(len(content)),
			OriginalSize: uint64(len(content)),
			Hash:         make([]byte, sha256.Size), // wrong hash
			Compression:  CompressionNone,
		}

		f := ops.OpenFile(entry, true)

		// Close without reading - should drain and find hash mismatch
		err := f.Close()
		if !errors.Is(err, ErrHashMismatch) {
			t.Errorf("Close() error = %v, want ErrHashMismatch", err)
		}
	})
}

func TestFile_Stat(t *testing.T) {
	t.Parallel()

	content := []byte("test content")
	source := newMockSource(content)
	ops := NewReader(source, WithMaxFileSize(0))

	entry := &Entry{
		Path:         "dir/test.txt",
		DataOffset:   0,
		DataSize:     uint64(len(content)),
		OriginalSize: uint64(len(content)),
		Hash:         hashOf(content),
		Compression:  CompressionNone,
		Mode:         0o644,
	}

	f := ops.OpenFile(entry, true)
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("Name() = %q, want %q", info.Name(), "test.txt")
	}
	if info.Size() != int64(len(content)) {
		t.Errorf("Size() = %d, want %d", info.Size(), len(content))
	}
	if info.Mode() != fs.FileMode(0o644) {
		t.Errorf("Mode() = %v, want %v", info.Mode(), fs.FileMode(0o644))
	}
	if info.IsDir() {
		t.Error("IsDir() = true, want false")
	}
}

func TestFile_EmptyFile(t *testing.T) {
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

	f := ops.OpenFile(entry, true)

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if len(got) != 0 {
		t.Errorf("ReadAll() = %q, want empty", got)
	}

	err = f.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestFile_IncrementalRead(t *testing.T) {
	t.Parallel()

	content := []byte("hello world, this is a longer piece of test content")
	source := newMockSource(content)
	ops := NewReader(source, WithMaxFileSize(0))

	entry := &Entry{
		Path:         "test.txt",
		DataOffset:   0,
		DataSize:     uint64(len(content)),
		OriginalSize: uint64(len(content)),
		Hash:         hashOf(content),
		Compression:  CompressionNone,
	}

	f := ops.OpenFile(entry, true)
	defer f.Close()

	// Read one byte at a time
	var result []byte
	buf := make([]byte, 1)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}

	if !bytes.Equal(result, content) {
		t.Errorf("Read() = %q, want %q", result, content)
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()

	entry := &Entry{
		Path:         "test.txt",
		OriginalSize: 100,
		Mode:         0o755,
	}

	info, err := NewInfo(entry, "test.txt")
	if err != nil {
		t.Fatalf("NewInfo() error = %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("Name() = %q, want %q", info.Name(), "test.txt")
	}
	if info.Size() != 100 {
		t.Errorf("Size() = %d, want %d", info.Size(), 100)
	}
	if info.Mode() != fs.FileMode(0o755) {
		t.Errorf("Mode() = %v, want %v", info.Mode(), fs.FileMode(0o755))
	}
	if info.IsDir() {
		t.Error("IsDir() = true, want false")
	}
	if info.Sys() != nil {
		t.Error("Sys() != nil")
	}
}
