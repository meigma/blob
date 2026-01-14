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

			w := NewWriter(WriteOptions{Compression: bc.compression})
			var indexBuf, dataBuf bytes.Buffer

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				indexBuf.Reset()
				dataBuf.Reset()
				if err := w.Create(context.Background(), dir, &indexBuf, &dataBuf); err != nil {
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
			idx, _ := createBenchArchive(b, dir, CompressionNone)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				path := paths[i%len(paths)]
				entry, ok := idx.LookupView(path)
				if !ok {
					b.Fatalf("missing entry for %q", path)
				}
				benchSinkEntry = entryFromViewWithPath(entry, path)
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
			idx, _ := createBenchArchive(b, dir, CompressionNone)

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
			idx, _ := createBenchArchive(b, dir, CompressionNone)
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
			idx, _ := createBenchArchive(b, dir, CompressionNone)
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

func BenchmarkReaderReadFile(b *testing.B) {
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
					idx, source := createBenchArchive(b, dir, compression)
					reader := NewReader(idx, source)

					if bc.fileSize > 0 {
						b.SetBytes(int64(bc.fileSize))
					}

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						path := paths[i%len(paths)]
						content, err := reader.ReadFile(path)
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

func createBenchArchive(b *testing.B, dir string, compression Compression) (*Index, *testutil.MockByteSource) {
	b.Helper()

	var indexBuf, dataBuf bytes.Buffer
	w := NewWriter(WriteOptions{Compression: compression})
	if err := w.Create(context.Background(), dir, &indexBuf, &dataBuf); err != nil {
		b.Fatal(err)
	}

	idx, err := LoadIndex(indexBuf.Bytes())
	if err != nil {
		b.Fatal(err)
	}

	return idx, testutil.NewMockByteSource(dataBuf.Bytes())
}
