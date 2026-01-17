package blob

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/meigma/blob/cache/disk"
	"github.com/meigma/blob/internal/testutil"
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
