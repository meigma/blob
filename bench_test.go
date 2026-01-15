package blob

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/index"
	"github.com/meigma/blob/internal/testutil"
)

var (
	benchSinkBytes []byte
	benchSinkEntry Entry
	benchSinkInt   int
)

type benchPattern string

const (
	benchPatternCompressible benchPattern = "compressible"
	benchPatternRandom       benchPattern = "random"

	benchDirCount = 16
)

func init() {
	if os.Getenv("BLOB_PROFILE_BLOCK") == "1" {
		runtime.SetBlockProfileRate(1)
	}
	if os.Getenv("BLOB_PROFILE_MUTEX") == "1" {
		runtime.SetMutexProfileFraction(1)
	}
}

func BenchmarkWriterCreate(b *testing.B) {
	cases := []struct {
		name        string
		fileCount   int
		fileSize    int
		compression Compression
		pattern     benchPattern
	}{
		{
			name:        "files=128/size=16k/none/compressible",
			fileCount:   128,
			fileSize:    16 << 10,
			compression: CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=128/size=16k/zstd/compressible",
			fileCount:   128,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=128/size=16k/zstd/random",
			fileCount:   128,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternRandom,
		},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, bc.pattern)

			totalBytes := int64(bc.fileCount * bc.fileSize)
			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			var indexBuf, dataBuf bytes.Buffer
			var opts []CreateOption
			if bc.compression != CompressionNone {
				opts = append(opts, CreateWithCompression(bc.compression))
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				indexBuf.Reset()
				dataBuf.Reset()
				if err := Create(context.Background(), dir, &indexBuf, &dataBuf, opts...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkIndexLookup(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
		fileSize  int
	}{
		{name: "files=256/size=4k", fileCount: 256, fileSize: 4 << 10},
		{name: "files=1024/size=4k", fileCount: 1024, fileSize: 4 << 10},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchPatternCompressible)
			idx := createBenchIndex(b, dir)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				path := paths[i%len(paths)]
				entry, ok := idx.LookupView(path)
				if !ok {
					b.Fatalf("missing entry for %q", path)
				}
				benchSinkEntry = blobtype.EntryFromViewWithPath(entry, path)
			}
		})
	}
}

func BenchmarkIndexLookupCopy(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
		fileSize  int
	}{
		{name: "files=256/size=4k", fileCount: 256, fileSize: 4 << 10},
		{name: "files=1024/size=4k", fileCount: 1024, fileSize: 4 << 10},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchPatternCompressible)
			idx := createBenchIndex(b, dir)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				path := paths[i%len(paths)]
				view, ok := idx.LookupView(path)
				if !ok {
					b.Fatalf("missing entry for %q", path)
				}
				benchSinkEntry = view.Entry()
			}
		})
	}
}

func BenchmarkEntriesWithPrefix(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
		fileSize  int
	}{
		{name: "files=256/size=4k", fileCount: 256, fileSize: 4 << 10},
		{name: "files=1024/size=4k", fileCount: 1024, fileSize: 4 << 10},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchPatternCompressible)
			idx := createBenchIndex(b, dir)
			prefix := "dir00/"

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				count := 0
				for range idx.EntriesWithPrefixView(prefix) {
					count++
				}
				if count == 0 {
					b.Fatal("expected at least one entry for prefix")
				}
				benchSinkInt = count
			}
		})
	}
}

func BenchmarkEntriesWithPrefixCopy(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
		fileSize  int
	}{
		{name: "files=256/size=4k", fileCount: 256, fileSize: 4 << 10},
		{name: "files=1024/size=4k", fileCount: 1024, fileSize: 4 << 10},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchPatternCompressible)
			idx := createBenchIndex(b, dir)
			prefix := "dir00/"

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				count := 0
				for view := range idx.EntriesWithPrefixView(prefix) {
					benchSinkEntry = view.Entry()
					count++
				}
				if count == 0 {
					b.Fatal("expected at least one entry for prefix")
				}
				benchSinkInt = count
			}
		})
	}
}

func BenchmarkBlobReadFile(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
		fileSize  int
	}{
		{name: "files=64/size=4k", fileCount: 64, fileSize: 4 << 10},
		{name: "files=64/size=64k", fileCount: 64, fileSize: 64 << 10},
		{name: "files=64/size=1m", fileCount: 64, fileSize: 1 << 20},
	}

	patterns := []benchPattern{benchPatternCompressible, benchPatternRandom}
	compressions := []Compression{CompressionNone, CompressionZstd}

	for _, bc := range cases {
		for _, pattern := range patterns {
			for _, compression := range compressions {
				name := fmt.Sprintf("%s/%s/%s", bc.name, pattern, compression.String())
				b.Run(name, func(b *testing.B) {
					dir := b.TempDir()
					paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, pattern)
					blob := createBenchBlob(b, dir, compression)

					if bc.fileSize > 0 {
						b.SetBytes(int64(bc.fileSize))
					}

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						path := paths[i%len(paths)]
						content, err := blob.ReadFile(path)
						if err != nil {
							b.Fatal(err)
						}
						benchSinkBytes = content
					}
				})
			}
		}
	}
}

func BenchmarkBlobCopyDir(b *testing.B) {
	benchmarkBlobCopyDir(b, "serial", -1)
	benchmarkBlobCopyDir(b, "parallel", runtime.GOMAXPROCS(0))
}

func benchmarkBlobCopyDir(b *testing.B, label string, workers int) {
	b.Helper()

	cases := []struct {
		name        string
		fileCount   int
		fileSize    int
		compression Compression
		pattern     benchPattern
	}{
		{
			name:        "files=512/size=16k/none/compressible",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=16k/zstd/compressible",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=16k/zstd/random",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=512/size=64k/zstd/compressible",
			fileCount:   512,
			fileSize:    64 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=64k/zstd/random",
			fileCount:   512,
			fileSize:    64 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=2048/size=16k/zstd/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
	}

	prefix := "dir00"

	for _, bc := range cases {
		b.Run(fmt.Sprintf("%s/%s", label, bc.name), func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, bc.pattern)
			blob := createBenchBlob(b, dir, bc.compression)

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			totalBytes := int64(dirEntries * bc.fileSize)
			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			opts := []CopyOption{}
			if workers != 0 {
				opts = append(opts, CopyWithWorkers(workers))
			}

			destRoot := b.TempDir()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				b.StopTimer()
				destDir := filepath.Join(destRoot, fmt.Sprintf("iter-%d", i))
				if err := os.MkdirAll(destDir, 0o755); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				if err := blob.CopyDir(destDir, prefix, opts...); err != nil {
					b.Fatal(err)
				}

				b.StopTimer()
				if err := os.RemoveAll(destDir); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

func makeBenchFiles(b *testing.B, dir string, fileCount, fileSize int, pattern benchPattern) []string {
	b.Helper()

	paths := make([]string, 0, fileCount)
	rng := rand.New(rand.NewSource(1))
	for i := range fileCount {
		relPath := fmt.Sprintf("dir%02d/file%05d.dat", i%benchDirCount, i)
		fullPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			b.Fatal(err)
		}

		content := make([]byte, fileSize)
		switch pattern {
		case benchPatternRandom:
			if _, err := rng.Read(content); err != nil {
				b.Fatal(err)
			}
		default:
			fillByte := byte('a' + (i % 26))
			for j := range content {
				content[j] = fillByte
			}
			if len(content) > 0 {
				content[0] = byte(i)
			}
		}

		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			b.Fatal(err)
		}
		paths = append(paths, relPath)
	}

	return paths
}

func countBenchDirEntries(fileCount, dirCount int) int {
	if fileCount <= 0 || dirCount <= 0 {
		return 0
	}
	return (fileCount + dirCount - 1) / dirCount
}

// createBenchIndex creates a test archive and returns the internal index for benchmarking.
// Index benchmarks always use CompressionNone since the index structure is identical regardless
// of data compression.
func createBenchIndex(b *testing.B, dir string) *index.Index {
	b.Helper()

	var indexBuf, dataBuf bytes.Buffer
	if err := Create(context.Background(), dir, &indexBuf, &dataBuf); err != nil {
		b.Fatal(err)
	}

	idx, err := index.Load(indexBuf.Bytes())
	if err != nil {
		b.Fatal(err)
	}

	return idx
}

// createBenchBlob creates a test archive and returns a Blob for benchmarking.
func createBenchBlob(b *testing.B, dir string, compression Compression) *Blob {
	b.Helper()

	var indexBuf, dataBuf bytes.Buffer
	var opts []CreateOption
	if compression != CompressionNone {
		opts = append(opts, CreateWithCompression(compression))
	}
	if err := Create(context.Background(), dir, &indexBuf, &dataBuf, opts...); err != nil {
		b.Fatal(err)
	}

	blob, err := New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBuf.Bytes()))
	if err != nil {
		b.Fatal(err)
	}

	return blob
}
