package fileops

import (
	"bytes"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func compressData(t *testing.T, data []byte) []byte {
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

func TestDecompressPool_Get(t *testing.T) {
	t.Parallel()

	original := []byte("hello world, this is a test of zstd compression")
	compressed := compressData(t, original)

	pool := NewDecompressPool(0)

	t.Run("basic decode", func(t *testing.T) {
		t.Parallel()
		dec, release, err := pool.Get(bytes.NewReader(compressed))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		defer release()

		result, err := io.ReadAll(dec)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		if !bytes.Equal(result, original) {
			t.Errorf("decoded = %q, want %q", result, original)
		}
	})

	t.Run("decoder reuse", func(t *testing.T) {
		t.Parallel()
		// Get and release multiple decoders to exercise reuse
		for i := range 5 {
			dec, release, err := pool.Get(bytes.NewReader(compressed))
			if err != nil {
				t.Fatalf("iteration %d: Get() error = %v", i, err)
			}

			result, err := io.ReadAll(dec)
			if err != nil {
				release()
				t.Fatalf("iteration %d: ReadAll() error = %v", i, err)
			}

			if !bytes.Equal(result, original) {
				release()
				t.Errorf("iteration %d: decoded = %q, want %q", i, result, original)
			}

			release()
		}
	})
}

func TestDecompressPool_WithMemoryLimit(t *testing.T) {
	t.Parallel()

	original := []byte("test data for memory limit test")
	compressed := compressData(t, original)

	pool := NewDecompressPool(1 << 20) // 1MB limit

	dec, release, err := pool.Get(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer release()

	result, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decoded = %q, want %q", result, original)
	}
}

func TestDecompressPool_NilPool(t *testing.T) {
	t.Parallel()

	original := []byte("test with nil pool")
	compressed := compressData(t, original)

	var pool *DecompressPool // nil pool

	dec, release, err := pool.Get(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer release()

	result, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decoded = %q, want %q", result, original)
	}
}

func TestDecompressPool_InvalidData(t *testing.T) {
	t.Parallel()

	pool := NewDecompressPool(0)

	// Invalid zstd data
	invalidData := []byte("this is not valid zstd data")

	dec, release, err := pool.Get(bytes.NewReader(invalidData))
	if err != nil {
		// Some implementations may fail on Get
		return
	}
	defer release()

	// Error should occur on read
	_, err = io.ReadAll(dec)
	if err == nil {
		t.Error("expected error reading invalid zstd data")
	}
}

func TestDecompressPool_Concurrent(t *testing.T) {
	t.Parallel()

	original := []byte("concurrent test data")
	compressed := compressData(t, original)

	pool := NewDecompressPool(0)

	const goroutines = 10
	const iterations = 100

	errCh := make(chan error, goroutines)

	for range goroutines {
		go func() {
			for range iterations {
				dec, release, err := pool.Get(bytes.NewReader(compressed))
				if err != nil {
					errCh <- err
					return
				}

				result, err := io.ReadAll(dec)
				release()

				if err != nil {
					errCh <- err
					return
				}

				if !bytes.Equal(result, original) {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}()
	}

	for range goroutines {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent error: %v", err)
		}
	}
}
