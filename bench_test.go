package blob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	blobhttp "github.com/meigma/blob/http"
	"github.com/meigma/blob/internal/blobtype"
	"github.com/meigma/blob/internal/index"
	"github.com/meigma/blob/internal/testutil"
)

var (
	benchSinkBytes []byte
	benchSinkEntry Entry
	benchSinkInt   int
	benchSinkFile  fs.File
	errBenchSink   error //nolint:errname // not a sentinel error, just a sink variable
	benchSinkInfo  fs.FileInfo
	benchSinkDirs  []fs.DirEntry
	benchSinkView  EntryView
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
					indexData, dataData := createBenchArchive(b, dir, compression)

					for _, source := range benchSources() {
						run := func(b *testing.B) { //nolint:thelper // not a test helper, closure for sub-benchmarks
							byteSource, cleanup, err := source.new(b, dataData)
							if err != nil {
								b.Fatal(err)
							}
							if cleanup != nil {
								defer cleanup()
							}

							blob, err := New(indexData, byteSource)
							if err != nil {
								b.Fatal(err)
							}

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
						}

						if source.name == "" {
							run(b)
						} else {
							b.Run(source.name, run)
						}
					}
				})
			}
		}
	}
}

func BenchmarkBlobReadFileHTTPMatrix(b *testing.B) {
	if !benchHTTPEnabled() {
		b.Skip("BLOB_BENCH_HTTP not set")
	}

	cases := []struct {
		name string
		cfg  benchHTTPConfig
	}{
		{name: "latency=0/bps=unlimited", cfg: benchHTTPConfig{}},
		{name: "latency=5ms/bps=10MBps", cfg: benchHTTPConfig{latency: 5 * time.Millisecond, bytesPerSecond: 10 * 1024 * 1024}},
		{name: "latency=20ms/bps=2MBps", cfg: benchHTTPConfig{latency: 20 * time.Millisecond, bytesPerSecond: 2 * 1024 * 1024}},
	}

	const fileCount = 64
	const fileSize = 64 << 10

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			source, cleanup, err := newBenchHTTPSourceWithConfig(b, dataData, bc.cfg)
			if err != nil {
				b.Fatal(err)
			}
			if cleanup != nil {
				defer cleanup()
			}

			blob, err := New(indexData, source)
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(fileSize)
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

func BenchmarkBlobReadFileDecoderOptions(b *testing.B) {
	configs := []struct {
		name string
		opts []Option
	}{
		{name: "default"},
		{name: "lowmem=false", opts: []Option{WithDecoderLowmem(false)}},
		{name: "concurrency=1", opts: []Option{WithDecoderConcurrency(1)}},
		{name: "concurrency=1/lowmem=false", opts: []Option{WithDecoderConcurrency(1), WithDecoderLowmem(false)}},
		{name: "concurrency=0", opts: []Option{WithDecoderConcurrency(0)}},
	}

	const fileCount = 64
	sizes := []int{
		64 << 10,
		1 << 20,
	}

	for _, size := range sizes {
		sizeName := fmt.Sprintf("size=%dk", size>>10)
		if size == 1<<20 {
			sizeName = "size=1m"
		}
		for _, cfg := range configs {
			b.Run(fmt.Sprintf("%s/%s", sizeName, cfg.name), func(b *testing.B) {
				dir := b.TempDir()
				paths := makeBenchFiles(b, dir, fileCount, size, benchPatternCompressible)
				blob := createBenchBlob(b, dir, CompressionZstd, cfg.opts...)

				b.SetBytes(int64(size))
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

func BenchmarkBlobCopyDirHTTPMatrix(b *testing.B) {
	if !benchHTTPEnabled() {
		b.Skip("BLOB_BENCH_HTTP not set")
	}

	cases := []struct {
		name string
		cfg  benchHTTPConfig
	}{
		{name: "latency=0/bps=unlimited", cfg: benchHTTPConfig{}},
		{name: "latency=5ms/bps=10MBps", cfg: benchHTTPConfig{latency: 5 * time.Millisecond, bytesPerSecond: 10 * 1024 * 1024}},
		{name: "latency=20ms/bps=2MBps", cfg: benchHTTPConfig{latency: 20 * time.Millisecond, bytesPerSecond: 2 * 1024 * 1024}},
	}

	const fileCount = 512
	const fileSize = 16 << 10
	const prefix = "dir00"

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)

	dirEntries := countBenchDirEntries(fileCount, benchDirCount)
	totalBytes := int64(dirEntries * fileSize)

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			source, cleanup, err := newBenchHTTPSourceWithConfig(b, dataData, bc.cfg)
			if err != nil {
				b.Fatal(err)
			}
			if cleanup != nil {
				defer cleanup()
			}

			blob, err := New(indexData, source)
			if err != nil {
				b.Fatal(err)
			}

			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			destRoot := b.TempDir()
			opts := []CopyOption{CopyWithCleanDest(true)}

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

func BenchmarkBlobOpen(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
	}{
		{name: "files=64", fileCount: 64},
		{name: "files=1024", fileCount: 1024},
	}

	const fileSize = 4 << 10

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, bc.fileCount, fileSize, benchPatternCompressible)
			blob := createBenchBlob(b, dir, CompressionZstd)
			missingPath := "missing/file.txt"
			dirPath := "dir00"

			b.Run("file", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; b.Loop(); i++ {
					path := paths[i%len(paths)]
					f, err := blob.Open(path)
					if err != nil {
						b.Fatal(err)
					}
					benchSinkFile = f
				}
			})

			b.Run("dir", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					f, err := blob.Open(dirPath)
					if err != nil {
						b.Fatal(err)
					}
					benchSinkFile = f
				}
			})

			b.Run("missing", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					f, err := blob.Open(missingPath)
					if err == nil {
						b.Fatal("expected error")
					}
					benchSinkFile = f
					errBenchSink = err
				}
			})
		})
	}
}

func BenchmarkBlobStat(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
	}{
		{name: "files=64", fileCount: 64},
		{name: "files=1024", fileCount: 1024},
	}

	const fileSize = 4 << 10

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, bc.fileCount, fileSize, benchPatternCompressible)
			blob := createBenchBlob(b, dir, CompressionZstd)
			missingPath := "missing/file.txt"
			dirPath := "dir00"

			b.Run("file", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; b.Loop(); i++ {
					path := paths[i%len(paths)]
					info, err := blob.Stat(path)
					if err != nil {
						b.Fatal(err)
					}
					benchSinkInfo = info
				}
			})

			b.Run("dir", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					info, err := blob.Stat(dirPath)
					if err != nil {
						b.Fatal(err)
					}
					benchSinkInfo = info
				}
			})

			b.Run("missing", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					info, err := blob.Stat(missingPath)
					if err == nil {
						b.Fatal("expected error")
					}
					benchSinkInfo = info
					errBenchSink = err
				}
			})
		})
	}
}

func BenchmarkBlobReadDir(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
	}{
		{name: "files=64", fileCount: 64},
		{name: "files=1024", fileCount: 1024},
	}

	const fileSize = 4 << 10

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, fileSize, benchPatternCompressible)
			blob := createBenchBlob(b, dir, CompressionNone)

			dirPath := "dir00"
			rootPath := "."
			missingPath := "missing"

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			rootEntries := bc.fileCount
			if rootEntries > benchDirCount {
				rootEntries = benchDirCount
			}

			b.Run("dir", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					entries, err := blob.ReadDir(dirPath)
					if err != nil {
						b.Fatal(err)
					}
					if len(entries) != dirEntries {
						b.Fatalf("unexpected entry count: %d", len(entries))
					}
					benchSinkDirs = entries
				}
			})

			b.Run("root", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					entries, err := blob.ReadDir(rootPath)
					if err != nil {
						b.Fatal(err)
					}
					if len(entries) != rootEntries {
						b.Fatalf("unexpected entry count: %d", len(entries))
					}
					benchSinkDirs = entries
				}
			})

			b.Run("missing", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					entries, err := blob.ReadDir(missingPath)
					if err == nil {
						b.Fatal("expected error")
					}
					benchSinkDirs = entries
					errBenchSink = err
				}
			})
		})
	}
}

func BenchmarkBlobEntry(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
	}{
		{name: "files=64", fileCount: 64},
		{name: "files=1024", fileCount: 1024},
	}

	const fileSize = 4 << 10

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, bc.fileCount, fileSize, benchPatternCompressible)
			blob := createBenchBlob(b, dir, CompressionNone)
			missingPath := "missing/file.txt"

			b.Run("hit", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; b.Loop(); i++ {
					path := paths[i%len(paths)]
					view, ok := blob.Entry(path)
					if !ok {
						b.Fatalf("missing entry for %q", path)
					}
					benchSinkView = view
				}
			})

			b.Run("miss", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					view, ok := blob.Entry(missingPath)
					if ok {
						b.Fatal("expected miss")
					}
					benchSinkView = view
				}
			})
		})
	}
}

func BenchmarkBlobCopyDir(b *testing.B) {
	benchmarkBlobCopyDir(b, "serial", -1, false)
	benchmarkBlobCopyDir(b, "serial-clean", -1, true)
	benchmarkBlobCopyDir(b, "parallel", runtime.GOMAXPROCS(0), false)
}

func benchmarkBlobCopyDir(b *testing.B, label string, workers int, cleanDest bool) {
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
			indexData, dataData := createBenchArchive(b, dir, bc.compression)

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			totalBytes := int64(dirEntries * bc.fileSize)

			for _, source := range benchSources() {
				run := func(b *testing.B) { //nolint:thelper // not a test helper, closure for sub-benchmarks
					byteSource, cleanup, err := source.new(b, dataData)
					if err != nil {
						b.Fatal(err)
					}
					if cleanup != nil {
						defer cleanup()
					}

					blob, err := New(indexData, byteSource)
					if err != nil {
						b.Fatal(err)
					}

					if totalBytes > 0 {
						b.SetBytes(totalBytes)
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

						opts := []CopyOption{}
						if workers != 0 {
							opts = append(opts, CopyWithWorkers(workers))
						}
						if cleanDest {
							opts = append(opts, CopyWithCleanDest(true))
						}

						if err := blob.CopyDir(destDir, prefix, opts...); err != nil {
							b.Fatal(err)
						}

						b.StopTimer()
						if err := os.RemoveAll(destDir); err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
					}
				}

				if source.name == "" {
					run(b)
				} else {
					b.Run(source.name, run)
				}
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

type benchSource struct {
	name string
	new  func(b *testing.B, data []byte) (ByteSource, func(), error)
}

func benchSources() []benchSource {
	sources := make([]benchSource, 0, 2)
	sources = append(sources, benchSource{
		new: func(_ *testing.B, data []byte) (ByteSource, func(), error) {
			return testutil.NewMockByteSource(data), nil, nil
		},
	})

	if !benchHTTPEnabled() {
		return sources
	}

	sources = append(sources, benchSource{
		name: "source=http",
		new:  newBenchHTTPSource,
	})
	return sources
}

func benchHTTPEnabled() bool {
	return os.Getenv("BLOB_BENCH_HTTP") != ""
}

//nolint:thelper // factory function, not a test helper
func newBenchHTTPSource(b *testing.B, data []byte) (ByteSource, func(), error) {
	cfg, err := benchHTTPConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	return newBenchHTTPSourceWithConfig(b, data, cfg)
}

//nolint:thelper,unparam // factory function, not a test helper; b kept for interface consistency
func newBenchHTTPSourceWithConfig(_ *testing.B, data []byte, cfg benchHTTPConfig) (ByteSource, func(), error) {
	client := benchHTTPClient(cfg)

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		nethttp.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	}))

	src, err := blobhttp.NewSource(server.URL, blobhttp.WithClient(client))
	if err != nil {
		server.Close()
		return nil, nil, err
	}

	cleanup := func() {
		server.Close()
	}
	return src, cleanup, nil
}

type benchHTTPConfig struct {
	latency        time.Duration
	bytesPerSecond int64
}

func benchHTTPClient(cfg benchHTTPConfig) *nethttp.Client {
	transport := nethttp.DefaultTransport
	if base, ok := transport.(*nethttp.Transport); ok {
		transport = base.Clone()
	}
	if cfg.latency > 0 || cfg.bytesPerSecond > 0 {
		transport = &benchHTTPRoundTripper{
			base:           transport,
			latency:        cfg.latency,
			bytesPerSecond: cfg.bytesPerSecond,
		}
	}
	return &nethttp.Client{Transport: transport}
}

func benchHTTPConfigFromEnv() (benchHTTPConfig, error) {
	var cfg benchHTTPConfig
	if value := strings.TrimSpace(os.Getenv("BLOB_HTTP_LATENCY")); value != "" {
		latency, err := time.ParseDuration(value)
		if err != nil {
			return cfg, fmt.Errorf("BLOB_HTTP_LATENCY: %w", err)
		}
		cfg.latency = latency
	}
	if value := strings.TrimSpace(os.Getenv("BLOB_HTTP_BPS")); value != "" {
		bps, err := parseBenchBytesPerSecond(value)
		if err != nil {
			return cfg, fmt.Errorf("BLOB_HTTP_BPS: %w", err)
		}
		cfg.bytesPerSecond = bps
	}
	return cfg, nil
}

func parseBenchBytesPerSecond(value string) (int64, error) {
	text := strings.TrimSpace(value)
	text = strings.TrimSuffix(text, "Bps")
	text = strings.TrimSuffix(text, "bps")
	text = strings.TrimSuffix(text, "/s")
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}

	lower := strings.ToLower(text)
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(lower, "kb"):
		multiplier = 1024
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "k"):
		multiplier = 1024
		text = text[:len(text)-1]
	case strings.HasSuffix(lower, "mb"):
		multiplier = 1024 * 1024
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "m"):
		multiplier = 1024 * 1024
		text = text[:len(text)-1]
	case strings.HasSuffix(lower, "gb"):
		multiplier = 1024 * 1024 * 1024
		text = text[:len(text)-2]
	case strings.HasSuffix(lower, "g"):
		multiplier = 1024 * 1024 * 1024
		text = text[:len(text)-1]
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}
	raw, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}
	if raw <= 0 {
		return 0, fmt.Errorf("invalid bytes-per-second %q", value)
	}
	return raw * multiplier, nil
}

type benchHTTPRoundTripper struct {
	base           nethttp.RoundTripper
	latency        time.Duration
	bytesPerSecond int64
}

func (rt *benchHTTPRoundTripper) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	if rt.latency > 0 {
		time.Sleep(rt.latency)
	}
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if rt.bytesPerSecond > 0 && resp.Body != nil {
		resp.Body = &benchThrottleReadCloser{
			rc:             resp.Body,
			bytesPerSecond: rt.bytesPerSecond,
			start:          time.Now(),
		}
	}
	return resp, nil
}

type benchThrottleReadCloser struct {
	rc             io.ReadCloser
	bytesPerSecond int64
	start          time.Time
	readBytes      int64
}

func (tr *benchThrottleReadCloser) Read(p []byte) (int, error) {
	n, err := tr.rc.Read(p)
	if n > 0 {
		tr.readBytes += int64(n)
		expected := time.Duration(float64(tr.readBytes) / float64(tr.bytesPerSecond) * float64(time.Second))
		elapsed := time.Since(tr.start)
		if expected > elapsed {
			time.Sleep(expected - elapsed)
		}
	}
	return n, err
}

func (tr *benchThrottleReadCloser) Close() error {
	return tr.rc.Close()
}

func createBenchArchive(b *testing.B, dir string, compression Compression) (indexData, dataData []byte) {
	b.Helper()

	var indexBuf, dataBuf bytes.Buffer
	var createOpts []CreateOption
	if compression != CompressionNone {
		createOpts = append(createOpts, CreateWithCompression(compression))
	}
	if err := Create(context.Background(), dir, &indexBuf, &dataBuf, createOpts...); err != nil {
		b.Fatal(err)
	}
	return indexBuf.Bytes(), dataBuf.Bytes()
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
func createBenchBlob(b *testing.B, dir string, compression Compression, blobOpts ...Option) *Blob {
	b.Helper()

	indexData, dataData := createBenchArchive(b, dir, compression)

	blob, err := New(indexData, testutil.NewMockByteSource(dataData), blobOpts...)
	if err != nil {
		b.Fatal(err)
	}

	return blob
}
