package blob

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	blobcache "github.com/meigma/blob/core/cache"
	"github.com/meigma/blob/core/cache/disk"
	"github.com/meigma/blob/core/testutil"
)

func BenchmarkBlobReadFileDiskCacheHit(b *testing.B) {
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

					for _, source := range benchCacheSources() {
						run := func(b *testing.B) { //nolint:thelper // not a test helper, closure for sub-benchmarks
							byteSource, cleanup, err := source.new(b, dataData)
							if err != nil {
								b.Fatal(err)
							}
							if cleanup != nil {
								defer cleanup()
							}

							cacheLabel := source.name
							if cacheLabel == "" {
								cacheLabel = "source=memory"
							}
							cacheDir := filepath.Join(dir, "cache", cacheLabel)
							diskCache, err := disk.New(cacheDir)
							if err != nil {
								b.Fatal(err)
							}

							cached, err := New(indexData, byteSource, WithCache(diskCache))
							if err != nil {
								b.Fatal(err)
							}

							for _, path := range paths {
								content, err := cached.ReadFile(path)
								if err != nil {
									b.Fatal(err)
								}
								benchSinkBytes = content
							}

							if bc.fileSize > 0 {
								b.SetBytes(int64(bc.fileSize))
							}

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

type benchCacheSource struct {
	name string
	new  func(b *testing.B, data []byte) (ByteSource, func(), error)
}

func benchCacheSources() []benchCacheSource {
	sources := []benchCacheSource{
		{
			new: func(_ *testing.B, data []byte) (ByteSource, func(), error) {
				return testutil.NewMockByteSource(data), nil, nil
			},
		},
	}

	if !benchHTTPEnabled() {
		return sources
	}

	httpCases := []struct {
		name string
		cfg  benchHTTPConfig
	}{
		{name: "latency=0/bps=unlimited", cfg: benchHTTPConfig{}},
		{name: "latency=5ms/bps=10MBps", cfg: benchHTTPConfig{latency: 5 * time.Millisecond, bytesPerSecond: 10 * 1024 * 1024}},
		{name: "latency=20ms/bps=2MBps", cfg: benchHTTPConfig{latency: 20 * time.Millisecond, bytesPerSecond: 2 * 1024 * 1024}},
	}

	for _, bc := range httpCases {
		sources = append(sources, benchCacheSource{
			name: "source=http/" + bc.name,
			new: func(b *testing.B, data []byte) (ByteSource, func(), error) {
				b.Helper()
				return newBenchHTTPSourceWithConfig(b, data, bc.cfg)
			},
		})
	}

	return sources
}

func BenchmarkBlobReadFileHTTPDiskCacheHit(b *testing.B) {
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

	for i, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			source, cleanup, err := newBenchHTTPSourceWithConfig(b, dataData, bc.cfg)
			if err != nil {
				b.Fatal(err)
			}
			if cleanup != nil {
				defer cleanup()
			}

			cacheDir := filepath.Join(dir, "cache", fmt.Sprintf("case-%d", i))
			diskCache, err := disk.New(cacheDir)
			if err != nil {
				b.Fatal(err)
			}
			cached, err := New(indexData, source, WithCache(diskCache))
			if err != nil {
				b.Fatal(err)
			}

			for _, path := range paths {
				content, err := cached.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = content
			}

			b.SetBytes(fileSize)
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
		})
	}
}

func BenchmarkBlobReadFileBlockCacheRandom(b *testing.B) {
	const fileCount = 1024
	const fileSize = 16 << 10

	blockSizes := []int64{64 << 10, 256 << 10}
	compressions := []Compression{CompressionNone, CompressionZstd}

	for _, compression := range compressions {
		name := fmt.Sprintf("files=%d/size=%dk/%s", fileCount, fileSize>>10, compression.String())
		b.Run(name, func(b *testing.B) {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternRandom)
			indexData, dataData := createBenchArchive(b, dir, compression)

			for _, source := range benchCacheSources() {
				for _, blockSize := range blockSizes {
					label := source.name
					if label == "" {
						label = "source=memory"
					}
					runName := fmt.Sprintf("%s/block=%dk", label, blockSize>>10)
					b.Run(runName, func(b *testing.B) {
						byteSource, cleanup, err := source.new(b, dataData)
						if err != nil {
							b.Fatal(err)
						}
						if cleanup != nil {
							defer cleanup()
						}

						cacheDir := filepath.Join(dir, "block-cache", label, fmt.Sprintf("block=%dk", blockSize>>10))
						cachedSource, err := wrapWithBlockCache(b, cacheDir, byteSource, blockSize, 0)
						if err != nil {
							b.Fatal(err)
						}

						cached, err := New(indexData, cachedSource)
						if err != nil {
							b.Fatal(err)
						}

						for _, path := range paths {
							content, err := cached.ReadFile(path)
							if err != nil {
								b.Fatal(err)
							}
							benchSinkBytes = content
						}

						b.SetBytes(fileSize)
						b.ReportAllocs()
						b.ResetTimer()
						var seed uint64 = 1
						for b.Loop() {
							seed = seed*1664525 + 1013904223
							path := paths[int(seed%uint64(len(paths)))]
							content, err := cached.ReadFile(path)
							if err != nil {
								b.Fatal(err)
							}
							benchSinkBytes = content
						}
					})
				}
			}
		})
	}
}

func BenchmarkBlobOpenBlockCacheRandomPartial(b *testing.B) {
	const fileCount = 1024
	const fileSize = 64 << 10
	const readSize = 4 << 10
	const blockSize = 256 << 10

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternRandom)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)

	for _, source := range benchCacheSources() {
		label := source.name
		if label == "" {
			label = "source=memory"
		}
		b.Run(label, func(b *testing.B) {
			byteSource, cleanup, err := source.new(b, dataData)
			if err != nil {
				b.Fatal(err)
			}
			if cleanup != nil {
				defer cleanup()
			}

			cacheDir := filepath.Join(dir, "block-cache", label, fmt.Sprintf("block=%dk", blockSize>>10))
			cachedSource, err := wrapWithBlockCache(b, cacheDir, byteSource, blockSize, 0)
			if err != nil {
				b.Fatal(err)
			}

			cached, err := New(indexData, cachedSource, WithVerifyOnClose(false))
			if err != nil {
				b.Fatal(err)
			}

			for _, path := range paths {
				content, err := cached.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = content
			}

			buf := make([]byte, readSize)
			b.SetBytes(readSize)
			b.ReportAllocs()
			b.ResetTimer()
			var seed uint64 = 7
			for b.Loop() {
				seed = seed*1664525 + 1013904223
				path := paths[int(seed%uint64(len(paths)))]
				f, err := cached.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				n, err := f.Read(buf)
				if err != nil {
					_ = f.Close()
					b.Fatal(err)
				}
				if err := f.Close(); err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = buf[:n]
			}
		})
	}
}

func BenchmarkBlobCopyDirBlockCache(b *testing.B) {
	const fileCount = 256
	const fileSize = 16 << 10

	type cacheCase struct {
		name             string
		enabled          bool
		blockSize        int64
		maxBlocksPerRead int
	}
	cacheCases := []cacheCase{
		{name: "block=off"},
		{name: "block=256k", enabled: true, blockSize: 256 << 10},
		{name: "block=256k/max=4", enabled: true, blockSize: 256 << 10, maxBlocksPerRead: 4},
	}

	dir := b.TempDir()
	_ = makeBenchFiles(b, dir, fileCount, fileSize, benchPatternRandom)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)

	for _, source := range benchCacheSources() {
		label := source.name
		if label == "" {
			label = "source=memory"
		}
		for _, cfg := range cacheCases {
			runName := fmt.Sprintf("%s/%s", label, cfg.name)
			b.Run(runName, func(b *testing.B) {
				byteSource, cleanup, err := source.new(b, dataData)
				if err != nil {
					b.Fatal(err)
				}
				if cleanup != nil {
					defer cleanup()
				}

				if cfg.enabled {
					cacheDir := filepath.Join(dir, "block-cache", label, cfg.name)
					byteSource, err = wrapWithBlockCache(b, cacheDir, byteSource, cfg.blockSize, cfg.maxBlocksPerRead)
					if err != nil {
						b.Fatal(err)
					}
				}

				cached, err := New(indexData, byteSource)
				if err != nil {
					b.Fatal(err)
				}

				destDir := filepath.Join(dir, "copy", label, cfg.name)
				if err := os.MkdirAll(destDir, 0o755); err != nil {
					b.Fatal(err)
				}

				if err := cached.CopyDir(destDir, ".", CopyWithOverwrite(true)); err != nil {
					b.Fatal(err)
				}

				b.SetBytes(int64(fileCount * fileSize))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := cached.CopyDir(destDir, ".", CopyWithOverwrite(true)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func wrapWithBlockCache(b *testing.B, dir string, src ByteSource, blockSize int64, maxBlocksPerRead int) (ByteSource, error) {
	b.Helper()

	blockCache, err := disk.NewBlockCache(dir)
	if err != nil {
		return nil, err
	}

	opts := []blobcache.WrapOption{blobcache.WithBlockSize(blockSize)}
	if maxBlocksPerRead > 0 {
		opts = append(opts, blobcache.WithMaxBlocksPerRead(maxBlocksPerRead))
	}

	return blockCache.Wrap(src, opts...)
}
