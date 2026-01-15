package disk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
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

	if err := c.Put(sum[:], content); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok := c.Get(sum[:])
	if !ok {
		t.Fatal("Get() ok = false, want true")
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

func TestCacheWriterCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := []byte("streamed")
	sum := sha256.Sum256(content)

	w, err := c.Writer(sum[:])
	if err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	got, ok := c.Get(sum[:])
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Get() content = %q, want %q", got, content)
	}
}

func TestCacheWriterDiscard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := []byte("discard")
	sum := sha256.Sum256(content)

	w, err := c.Writer(sum[:])
	if err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := w.Discard(); err != nil {
		t.Fatalf("Discard() error = %v", err)
	}

	if got, ok := c.Get(sum[:]); ok {
		t.Fatalf("Get() ok = true, want false (content %q)", got)
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

	if err := c.Put(sum[:], content); err != nil {
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
