//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fs.FS Operations ---

func TestArchive_ReadFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "archive-readfile")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	for path, expected := range smallArchive {
		content, err := archive.ReadFile(path)
		require.NoError(t, err, "ReadFile(%q)", path)
		assert.Equal(t, expected, content, "content of %q", path)
	}
}

func TestArchive_ReadFile_Compressed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, compressibleArchive)

	ref := testRef(registryAddr, "archive-readfile-compressed")
	err := client.Push(ctx, ref, dir, blob.PushWithCompression(blob.CompressionZstd))
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	for path, expected := range compressibleArchive {
		content, err := archive.ReadFile(path)
		require.NoError(t, err, "ReadFile(%q)", path)
		assert.Equal(t, expected, content, "content of %q", path)
	}
}

func TestArchive_ReadDir(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "archive-readdir")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	// Root directory
	entries, err := archive.ReadDir(".")
	require.NoError(t, err, "ReadDir root")

	names := extractNames(entries)
	assert.Contains(t, names, "root.txt")
	assert.Contains(t, names, "dir1")
	assert.Contains(t, names, "dir2")
	assert.Contains(t, names, "empty")
}

func TestArchive_ReadDir_Nested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "archive-readdir-nested")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	t.Run("dir1", func(t *testing.T) {
		t.Parallel()
		entries, err := archive.ReadDir("dir1")
		require.NoError(t, err)

		names := extractNames(entries)
		assert.ElementsMatch(t, []string{"a.txt", "b.txt", "sub"}, names)
	})

	t.Run("dir1/sub", func(t *testing.T) {
		t.Parallel()
		entries, err := archive.ReadDir("dir1/sub")
		require.NoError(t, err)

		names := extractNames(entries)
		assert.ElementsMatch(t, []string{"c.txt"}, names)
	})

	t.Run("dir2/deep", func(t *testing.T) {
		t.Parallel()
		entries, err := archive.ReadDir("dir2/deep")
		require.NoError(t, err)

		names := extractNames(entries)
		assert.ElementsMatch(t, []string{"y.txt", "z.txt"}, names)
	})
}

func TestArchive_Open(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "archive-open")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	f, err := archive.Open("hello.txt")
	require.NoError(t, err, "Open")
	defer f.Close()

	content, err := io.ReadAll(f)
	require.NoError(t, err, "ReadAll")
	assert.Equal(t, []byte("Hello, World!"), content)
}

func TestArchive_Open_ChunkedRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create large compressible content
	largeContent := makeCompressibleContent(100 * 1024)
	files := map[string][]byte{
		"stream.txt": largeContent,
	}

	dir := t.TempDir()
	createTestFiles(t, dir, files)

	ref := testRef(registryAddr, "archive-open-chunked")
	err := client.Push(ctx, ref, dir, blob.PushWithCompression(blob.CompressionZstd))
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	f, err := archive.Open("stream.txt")
	require.NoError(t, err, "Open")
	defer f.Close()

	// Read in chunks to simulate streaming
	var buf bytes.Buffer
	chunk := make([]byte, 4096)
	for {
		n, err := f.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "Read chunk")
	}

	assert.Equal(t, largeContent, buf.Bytes())
}

func TestArchive_Stat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "archive-stat")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		info, err := archive.Stat("hello.txt")
		require.NoError(t, err, "Stat file")

		assert.Equal(t, "hello.txt", info.Name())
		assert.Equal(t, int64(13), info.Size()) // "Hello, World!"
		assert.False(t, info.IsDir())
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		// Create files with a directory structure
		files := map[string][]byte{
			"dir/file.txt": []byte("content"),
		}
		dir2 := t.TempDir()
		createTestFiles(t, dir2, files)

		ref2 := testRef(registryAddr, "archive-stat-dir")
		err := client.Push(ctx, ref2, dir2)
		require.NoError(t, err, "Push")

		archive2, err := client.Pull(ctx, ref2)
		require.NoError(t, err, "Pull")

		info, err := archive2.Stat("dir")
		require.NoError(t, err, "Stat directory")

		assert.Equal(t, "dir", info.Name())
		assert.True(t, info.IsDir())
	})
}

func TestArchive_FSWalk(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "archive-fswalk")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	var paths []string
	err = fs.WalkDir(archive, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	require.NoError(t, err, "WalkDir")

	// Should have found all files
	assert.Len(t, paths, len(nestedArchive))
}

// --- Entry Iteration ---

func TestArchive_Entry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "archive-entry")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	view, ok := archive.Entry("hello.txt")
	require.True(t, ok, "Entry found")
	assert.Equal(t, "hello.txt", view.Path())
	assert.Equal(t, uint64(13), view.OriginalSize()) // "Hello, World!"
}

func TestArchive_Entries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "archive-entries")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	count := 0
	for range archive.Entries() {
		count++
	}
	assert.Equal(t, len(smallArchive), count)
}

func TestArchive_EntriesWithPrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "archive-entries-prefix")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	count := 0
	for range archive.EntriesWithPrefix("dir1/") {
		count++
	}
	// dir1/a.txt, dir1/b.txt, dir1/sub/c.txt
	assert.Equal(t, 3, count)
}

func TestArchive_Len(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "archive-len")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	assert.Equal(t, len(nestedArchive), archive.Len())
}

// --- Extraction Tests ---

func TestCopyTo_SpecificFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "copyto-specific")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()
	_, err = archive.CopyTo(destDir, "root.txt", "dir1/a.txt")
	require.NoError(t, err, "CopyTo")

	// Verify extracted files
	content, err := os.ReadFile(filepath.Join(destDir, "root.txt"))
	require.NoError(t, err)
	assert.Equal(t, nestedArchive["root.txt"], content)

	content, err = os.ReadFile(filepath.Join(destDir, "dir1", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, nestedArchive["dir1/a.txt"], content)

	// Files not extracted should not exist
	_, err = os.Stat(filepath.Join(destDir, "dir2", "x.txt"))
	assert.True(t, os.IsNotExist(err))
}

func TestCopyTo_WithOverwrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "copyto-overwrite")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()

	// Pre-create file with different content
	existing := filepath.Join(destDir, "hello.txt")
	require.NoError(t, os.WriteFile(existing, []byte("original"), 0o644))

	// Without overwrite - file should not be overwritten
	_, err = archive.CopyTo(destDir, "hello.txt")
	require.NoError(t, err, "CopyTo without overwrite")

	content, err := os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), content)

	// With overwrite - file should be overwritten
	_, err = archive.CopyToWithOptions(destDir, []string{"hello.txt"}, blob.CopyWithOverwrite(true))
	require.NoError(t, err, "CopyTo with overwrite")

	content, err = os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, smallArchive["hello.txt"], content)
}

func TestCopyTo_WithPreserveMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create file with specific mode
	files := map[string][]byte{
		"script.sh": []byte("#!/bin/bash\necho hello"),
	}

	dir := t.TempDir()
	createTestFiles(t, dir, files)

	// Set executable mode
	require.NoError(t, os.Chmod(filepath.Join(dir, "script.sh"), 0o755))

	ref := testRef(registryAddr, "copyto-preserve-mode")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()
	_, err = archive.CopyToWithOptions(destDir, []string{"script.sh"}, blob.CopyWithPreserveMode(true))
	require.NoError(t, err, "CopyTo with preserve mode")

	info, err := os.Stat(filepath.Join(destDir, "script.sh"))
	require.NoError(t, err)
	// Mode should have execute bit
	assert.NotZero(t, info.Mode()&0o111, "should have execute permission")
}

func TestCopyTo_WithPreserveTimes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, smallArchive)

	ref := testRef(registryAddr, "copyto-preserve-times")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()
	_, err = archive.CopyToWithOptions(destDir, []string{"hello.txt"}, blob.CopyWithPreserveTimes(true))
	require.NoError(t, err, "CopyTo with preserve times")

	// File should exist (detailed time verification would require known mtime)
	_, err = os.Stat(filepath.Join(destDir, "hello.txt"))
	require.NoError(t, err)
}

func TestCopyDir_Prefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "copydir-prefix")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()
	_, err = archive.CopyDir(destDir, "dir1")
	require.NoError(t, err, "CopyDir")

	// dir1 files should exist
	assertDirContents(t, destDir, map[string][]byte{
		"dir1/a.txt":     nestedArchive["dir1/a.txt"],
		"dir1/b.txt":     nestedArchive["dir1/b.txt"],
		"dir1/sub/c.txt": nestedArchive["dir1/sub/c.txt"],
	})

	// Other files should not exist
	_, err = os.Stat(filepath.Join(destDir, "root.txt"))
	assert.True(t, os.IsNotExist(err))
}

func TestCopyDir_All(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "copydir-all")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()
	_, err = archive.CopyDir(destDir, "")
	require.NoError(t, err, "CopyDir all")

	assertDirContents(t, destDir, nestedArchive)
}

func TestCopyDir_WithCleanDest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	dir := t.TempDir()
	createTestFiles(t, dir, nestedArchive)

	ref := testRef(registryAddr, "copydir-clean")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()

	// Pre-create some files that should be removed
	oldFile := filepath.Join(destDir, "dir1", "old.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(oldFile), 0o755))
	require.NoError(t, os.WriteFile(oldFile, []byte("old"), 0o644))

	_, err = archive.CopyDir(destDir, "dir1", blob.CopyWithCleanDest(true))
	require.NoError(t, err, "CopyDir with clean")

	// Old file should be gone
	_, err = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err))

	// New files should exist
	assertDirContents(t, destDir, map[string][]byte{
		"dir1/a.txt":     nestedArchive["dir1/a.txt"],
		"dir1/b.txt":     nestedArchive["dir1/b.txt"],
		"dir1/sub/c.txt": nestedArchive["dir1/sub/c.txt"],
	})
}

func TestCopyDir_WithWorkers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registryAddr := getRegistry(t)
	client := newTestClient(t, registryAddr)

	// Create more files to benefit from parallelism
	manyFiles := make(map[string][]byte)
	for i := range 20 {
		manyFiles[filepath.Join("data", string(rune('a'+i%26))+".txt")] = makeRandomContent(1024)
	}

	dir := t.TempDir()
	createTestFiles(t, dir, manyFiles)

	ref := testRef(registryAddr, "copydir-workers")
	err := client.Push(ctx, ref, dir)
	require.NoError(t, err, "Push")

	archive, err := client.Pull(ctx, ref)
	require.NoError(t, err, "Pull")

	destDir := t.TempDir()
	_, err = archive.CopyDir(destDir, "data", blob.CopyWithWorkers(4))
	require.NoError(t, err, "CopyDir with workers")

	// Verify all files were copied
	entries, err := os.ReadDir(filepath.Join(destDir, "data"))
	require.NoError(t, err)
	assert.Len(t, entries, 20)
}
