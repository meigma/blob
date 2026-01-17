package disk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePutGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := []byte("hello")
	sum := sha256.Sum256(content)

	// Create a bytes file for Put
	bf := &bytesFile{Reader: bytes.NewReader(content)}
	if putErr := c.Put(sum[:], bf); putErr != nil {
		t.Fatalf("Put() error = %v", putErr)
	}

	f, ok := c.Get(sum[:])
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Get() content = %q, want %q", got, content)
	}

	hexHash := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, hexHash[:defaultShardPrefixLen], hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestCacheShardDisable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir, WithShardPrefixLen(0))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := []byte("flat")
	sum := sha256.Sum256(content)

	bf := &bytesFile{Reader: bytes.NewReader(content)}
	if err := c.Put(sum[:], bf); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	hexHash := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, hexHash)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s: %v", path, err)
	}
}

func TestNewEmptyDir(t *testing.T) {
	t.Parallel()

	if _, err := New(""); err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestCacheAlreadyCached(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := []byte("cached twice")
	sum := sha256.Sum256(content)

	// First put
	bf1 := &bytesFile{Reader: bytes.NewReader(content)}
	if putErr := c.Put(sum[:], bf1); putErr != nil {
		t.Fatalf("Put() error = %v", putErr)
	}

	// Second put should succeed (no-op)
	bf2 := &bytesFile{Reader: bytes.NewReader(content)}
	if putErr := c.Put(sum[:], bf2); putErr != nil {
		t.Fatalf("Put() error = %v (should be no-op)", putErr)
	}

	f, ok := c.Get(sum[:])
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Get() content = %q, want %q", got, content)
	}
}

// bytesFile wraps a bytes.Reader for testing Put.
type bytesFile struct {
	*bytes.Reader
}

func (f *bytesFile) Stat() (os.FileInfo, error) {
	return &testFileInfo{size: int64(f.Len())}, nil
}
func (f *bytesFile) Close() error { return nil }

type testFileInfo struct {
	size int64
}

func (fi *testFileInfo) Name() string       { return "" }
func (fi *testFileInfo) Size() int64        { return fi.size }
func (fi *testFileInfo) Mode() os.FileMode  { return 0o644 }
func (fi *testFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *testFileInfo) IsDir() bool        { return false }
func (fi *testFileInfo) Sys() any           { return nil }
