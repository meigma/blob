package cache

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/meigma/blob"
	"github.com/meigma/blob/internal/testutil"
)

var benchSinkBytes []byte

type benchPattern string

const (
	benchPatternCompressible benchPattern = "compressible"
	benchPatternRandom       benchPattern = "random"

	benchDirCount = 16
)

func BenchmarkReaderReadFileHit(b *testing.B) {
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, 128, 32<<10, benchDirCount, benchPatternCompressible)
	idx, source := createBenchArchive(b, dir, blob.CompressionZstd)
	reader := blob.NewReader(idx, source)
	mockCache := testutil.NewMockCache()
	cached := NewReader(reader, mockCache)

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

func BenchmarkReaderPrefetchDir(b *testing.B) {
	cases := []struct {
		name        string
		fileCount   int
		fileSize    int
		compression blob.Compression
		pattern     benchPattern
	}{
		{
			name:        "files=512/size=16k/none/compressible",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: blob.CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=16k/zstd/compressible",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=16k/zstd/random",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=512/size=64k/zstd/compressible",
			fileCount:   512,
			fileSize:    64 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=64k/zstd/random",
			fileCount:   512,
			fileSize:    64 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=2048/size=16k/zstd/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
	}

	prefix := "dir00"

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, bc.pattern)
			idx, source := createBenchArchive(b, dir, bc.compression)
			reader := blob.NewReader(idx, source)

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			totalBytes := int64(dirEntries * bc.fileSize)
			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				mockCache := testutil.NewMockCache()
				cached := NewReader(reader, mockCache)
				b.StartTimer()

				if err := cached.PrefetchDir(prefix); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReaderPrefetchDirDisk(b *testing.B) {
	benchmarkReaderPrefetchDirDisk(b, "serial", 1)
}

func BenchmarkReaderPrefetchDirDiskParallel(b *testing.B) {
	benchmarkReaderPrefetchDirDisk(b, "parallel", runtime.GOMAXPROCS(0))
}

func benchmarkReaderPrefetchDirDisk(b *testing.B, label string, workers int) {
	b.Helper()
	cases := []struct {
		name        string
		fileCount   int
		fileSize    int
		compression blob.Compression
		pattern     benchPattern
	}{
		{
			name:        "files=512/size=16k/none/compressible",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: blob.CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=16k/zstd/compressible",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=16k/zstd/random",
			fileCount:   512,
			fileSize:    16 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=512/size=64k/zstd/compressible",
			fileCount:   512,
			fileSize:    64 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=512/size=64k/zstd/random",
			fileCount:   512,
			fileSize:    64 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=2048/size=16k/zstd/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
	}

	prefix := "dir00"

	for _, bc := range cases {
		b.Run(fmt.Sprintf("%s/%s", label, bc.name), func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, bc.pattern)
			idx, source := createBenchArchive(b, dir, bc.compression)
			reader := blob.NewReader(idx, source)
			cachedOpts := []ReaderOption{WithPrefetchConcurrency(workers)}

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
				diskCache := newBenchDiskCache(cacheDir)
				cached := NewReader(reader, diskCache, cachedOpts...)
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
	for i := range fileCount {
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

func createBenchArchive(b *testing.B, dir string, compression blob.Compression) (*blob.Index, *testutil.MockByteSource) {
	b.Helper()

	var indexBuf, dataBuf bytes.Buffer
	w := blob.NewWriter(blob.WriteOptions{Compression: compression})
	if err := w.Create(context.Background(), dir, &indexBuf, &dataBuf); err != nil {
		b.Fatal(err)
	}

	idx, err := blob.LoadIndex(indexBuf.Bytes())
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

func (c *benchDiskCache) Writer(hash []byte) (Writer, error) {
	writer, err := c.DiskCache.Writer(hash)
	if err != nil {
		return nil, err
	}
	adapted, ok := writer.(Writer)
	if !ok {
		return nil, fmt.Errorf("unexpected cache writer type %T", writer)
	}
	return adapted, nil
}
