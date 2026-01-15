package file

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
)

func TestHashingReader(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	expectedHash := sha256.Sum256(data)

	t.Run("computes correct hash", func(t *testing.T) {
		t.Parallel()
		r := bytes.NewReader(data)
		hr := NewHashingReader(r, sha256.New())

		result, err := io.ReadAll(hr)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		if !bytes.Equal(result, data) {
			t.Errorf("data = %q, want %q", result, data)
		}

		sum := hr.Sum()
		if !bytes.Equal(sum, expectedHash[:]) {
			t.Errorf("hash = %x, want %x", sum, expectedHash)
		}
	})

	t.Run("incremental reads produce same hash", func(t *testing.T) {
		t.Parallel()
		r := bytes.NewReader(data)
		hr := NewHashingReader(r, sha256.New())

		// Read one byte at a time
		buf := make([]byte, 1)
		var result []byte
		for {
			n, err := hr.Read(buf)
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

		if !bytes.Equal(result, data) {
			t.Errorf("data = %q, want %q", result, data)
		}

		sum := hr.Sum()
		if !bytes.Equal(sum, expectedHash[:]) {
			t.Errorf("hash = %x, want %x", sum, expectedHash)
		}
	})

	t.Run("empty reader", func(t *testing.T) {
		t.Parallel()
		r := bytes.NewReader(nil)
		hr := NewHashingReader(r, sha256.New())

		result, err := io.ReadAll(hr)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		if len(result) != 0 {
			t.Errorf("expected empty result, got %q", result)
		}

		// SHA256 of empty string
		emptyHash := sha256.Sum256(nil)
		sum := hr.Sum()
		if !bytes.Equal(sum, emptyHash[:]) {
			t.Errorf("hash = %x, want %x", sum, emptyHash)
		}
	})
}

type errReader struct {
	err error
}

func (r *errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func TestHashingReader_Error(t *testing.T) {
	t.Parallel()

	testErr := errors.New("test error")
	r := &errReader{err: testErr}
	hr := NewHashingReader(r, sha256.New())

	buf := make([]byte, 10)
	_, err := hr.Read(buf)
	if err != testErr {
		t.Errorf("Read() error = %v, want %v", err, testErr)
	}
}

func TestEnsureNoExtra(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{
			name:    "empty reader",
			data:    nil,
			wantErr: nil,
		},
		{
			name:    "has extra data",
			data:    []byte("extra"),
			wantErr: ErrSizeOverflow,
		},
		{
			name:    "single extra byte",
			data:    []byte{0},
			wantErr: ErrSizeOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(tt.data)
			err := EnsureNoExtra(r)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("EnsureNoExtra() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureNoExtra_Error(t *testing.T) {
	t.Parallel()

	testErr := errors.New("read error")
	r := &errReader{err: testErr}

	err := EnsureNoExtra(r)
	if err != testErr {
		t.Errorf("EnsureNoExtra() error = %v, want %v", err, testErr)
	}
}
