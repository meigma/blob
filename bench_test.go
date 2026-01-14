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
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, bc.pattern)

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
			paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, benchPatternCompressible)
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
			paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, benchPatternCompressible)
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
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, benchPatternCompressible)
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
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, benchPatternCompressible)
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
					paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, pattern)
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

func BenchmarkCachedReaderReadFileHit(b *testing.B) {
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, 128, 32<<10, benchDirCount, benchPatternCompressible)
	idx, source := createBenchArchive(b, dir, CompressionZstd)
	reader := NewReader(idx, source)
	cache := testutil.NewMockCache()
	cached := NewCachedReader(reader, cache)

	for _, path := range paths {
		content, err := cached.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
	}

	b.SetBytes(32 << 10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		path := paths[i%len(paths)]
		content, err := cached.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
	}
}

func BenchmarkCachedReaderPrefetchDir(b *testing.B) {
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
			name:        "files=2048/size=16k/none/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=2048/size=16k/zstd/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=128/size=1m/none/compressible",
			fileCount:   128,
			fileSize:    1 << 20,
			compression: CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=128/size=1m/zstd/compressible",
			fileCount:   128,
			fileSize:    1 << 20,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=64/size=4m/zstd/compressible",
			fileCount:   64,
			fileSize:    4 << 20,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
	}

	prefix := "dir00"

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, bc.pattern)
			idx, source := createBenchArchive(b, dir, bc.compression)
			reader := NewReader(idx, source)

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			totalBytes := int64(dirEntries * bc.fileSize)
			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				cache := testutil.NewMockCache()
				cached := NewCachedReader(reader, cache)
				b.StartTimer()

				if err := cached.PrefetchDir(prefix); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCachedReaderPrefetchDirDisk(b *testing.B) {
	benchmarkCachedReaderPrefetchDirDisk(b, "serial", 1)
}

func BenchmarkCachedReaderPrefetchDirDiskParallel(b *testing.B) {
	benchmarkCachedReaderPrefetchDirDisk(b, "parallel", runtime.GOMAXPROCS(0))
}

func benchmarkCachedReaderPrefetchDirDisk(b *testing.B, label string, workers int) {
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
			name:        "files=2048/size=16k/none/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=2048/size=16k/zstd/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=128/size=1m/none/compressible",
			fileCount:   128,
			fileSize:    1 << 20,
			compression: CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=128/size=1m/zstd/compressible",
			fileCount:   128,
			fileSize:    1 << 20,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=64/size=4m/zstd/compressible",
			fileCount:   64,
			fileSize:    4 << 20,
			compression: CompressionZstd,
			pattern:     benchPatternCompressible,
		},
	}

	prefix := "dir00"

	for _, bc := range cases {
		b.Run(fmt.Sprintf("%s/%s", label, bc.name), func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, bc.pattern)
			idx, source := createBenchArchive(b, dir, bc.compression)
			reader := NewReader(idx, source)
			cachedOpts := []CachedReaderOption{WithPrefetchConcurrency(workers)}

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			totalBytes := int64(dirEntries * bc.fileSize)
			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			cacheRoot := filepath.Join(dir, "cache")
			if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				b.StopTimer()
				cacheDir := filepath.Join(cacheRoot, fmt.Sprintf("iter-%d", i))
				if err := os.MkdirAll(cacheDir, 0o755); err != nil {
					b.Fatal(err)
				}
				cache := newBenchDiskCache(cacheDir)
				cached := NewCachedReader(reader, cache, cachedOpts...)
				b.StartTimer()

				if err := cached.PrefetchDir(prefix); err != nil {
					b.Fatal(err)
				}

				b.StopTimer()
				if err := os.RemoveAll(cacheDir); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

func makeBenchFiles(b *testing.B, dir string, fileCount, fileSize, dirCount int, pattern benchPattern) []string {
	b.Helper()

	if dirCount <= 0 {
		dirCount = 1
	}

	paths := make([]string, 0, fileCount)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < fileCount; i++ {
		relPath := fmt.Sprintf("dir%02d/file%05d.dat", i%dirCount, i)
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

type benchDiskCache struct {
	*testutil.DiskCache
}

func newBenchDiskCache(dir string) *benchDiskCache {
	return &benchDiskCache{DiskCache: testutil.NewDiskCache(dir)}
}

func (c *benchDiskCache) Writer(hash []byte) (CacheWriter, error) {
	writer, err := c.DiskCache.Writer(hash)
	if err != nil {
		return nil, err
	}
	adapted, ok := writer.(CacheWriter)
	if !ok {
		return nil, fmt.Errorf("unexpected cache writer type %T", writer)
	}
	return adapted, nil
}
