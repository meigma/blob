package cache_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

	"github.com/meigma/blob"
	"github.com/meigma/blob/cache"
	"github.com/meigma/blob/cache/disk"
	blobhttp "github.com/meigma/blob/http"
	"github.com/meigma/blob/internal/testutil"
)

var benchSinkBytes []byte

type benchPattern string

const (
	benchPatternCompressible benchPattern = "compressible"
	benchPatternRandom       benchPattern = "random"

	benchDirCount = 16
)

func BenchmarkCachedBlobReadFileHit(b *testing.B) {
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, 128, 32<<10, benchDirCount, benchPatternCompressible)
	base := createBenchBlob(b, dir, blob.CompressionZstd)
	mockCache := testutil.NewMockCache()
	cached := cache.New(base, mockCache)

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

func BenchmarkCachedBlobReadFileHTTPDiskHit(b *testing.B) {
	if !benchHTTPEnabled() {
		b.Skip("BLOB_BENCH_HTTP not set")
	}

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, 128, 32<<10, benchDirCount, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, blob.CompressionZstd)

	source, cleanup := newBenchHTTPSource(b, dataData)
	defer cleanup()

	base, err := blob.New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	cacheDir := filepath.Join(dir, "cache")
	diskCache, err := disk.New(cacheDir)
	if err != nil {
		b.Fatal(err)
	}
	cached := cache.New(base, diskCache)

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

func BenchmarkCachedBlobPrefetchDir(b *testing.B) {
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
			name:        "files=2048/size=16k/none/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: blob.CompressionNone,
			pattern:     benchPatternCompressible,
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
			base := createBenchBlob(b, dir, bc.compression)

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
				cached := cache.New(base, mockCache)
				b.StartTimer()

				if err := cached.PrefetchDir(prefix); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCachedBlobPrefetchDirHTTPDisk(b *testing.B) {
	if !benchHTTPEnabled() {
		b.Skip("BLOB_BENCH_HTTP not set")
	}
	benchmarkBlobPrefetchDirHTTPDisk(b)
}

func BenchmarkCachedBlobPrefetchDirDisk(b *testing.B) {
	benchmarkBlobPrefetchDirDisk(b, "mode=serial", 1)
}

func BenchmarkCachedBlobPrefetchDirDiskParallel(b *testing.B) {
	benchmarkBlobPrefetchDirDisk(b, "mode=parallel", runtime.GOMAXPROCS(0))
}

func benchmarkBlobPrefetchDirDisk(b *testing.B, label string, workers int) {
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
			name:        "files=2048/size=16k/none/compressible",
			fileCount:   2048,
			fileSize:    16 << 10,
			compression: blob.CompressionNone,
			pattern:     benchPatternCompressible,
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
			base := createBenchBlob(b, dir, bc.compression)
			cachedOpts := []cache.Option{cache.WithPrefetchConcurrency(workers)}

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
				diskCache, err := disk.New(cacheDir)
				if err != nil {
					b.Fatal(err)
				}
				cached := cache.New(base, diskCache, cachedOpts...)
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

func benchmarkBlobPrefetchDirHTTPDisk(b *testing.B) {
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
		{
			name:        "files=32/size=1m/zstd/compressible",
			fileCount:   32,
			fileSize:    1 << 20,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=32/size=1m/zstd/random",
			fileCount:   32,
			fileSize:    1 << 20,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
		{
			name:        "files=8/size=4m/zstd/compressible",
			fileCount:   8,
			fileSize:    4 << 20,
			compression: blob.CompressionZstd,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=8/size=4m/zstd/random",
			fileCount:   8,
			fileSize:    4 << 20,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
	}

	prefix := "dir00"
	modes := []struct {
		label   string
		workers int
	}{
		{label: "mode=serial", workers: 1},
		{label: "mode=parallel", workers: runtime.GOMAXPROCS(0)},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, benchDirCount, bc.pattern)
			indexData, dataData := createBenchArchive(b, dir, bc.compression)

			source, cleanup := newBenchHTTPSource(b, dataData)
			defer cleanup()

			base, err := blob.New(indexData, source)
			if err != nil {
				b.Fatal(err)
			}

			dirEntries := countBenchDirEntries(bc.fileCount, benchDirCount)
			totalBytes := int64(dirEntries * bc.fileSize)
			if totalBytes > 0 {
				b.SetBytes(totalBytes)
			}

			cacheRoot := filepath.Join(dir, "cache")
			if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
				b.Fatal(err)
			}

			for _, mode := range modes {
				b.Run(mode.label, func(b *testing.B) {
					cachedOpts := []cache.Option{cache.WithPrefetchConcurrency(mode.workers)}

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						b.StopTimer()
						cacheDir := filepath.Join(cacheRoot, fmt.Sprintf("iter-%d", i))
						diskCache, err := disk.New(cacheDir)
						if err != nil {
							b.Fatal(err)
						}
						cached := cache.New(base, diskCache, cachedOpts...)
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

func createBenchBlob(b *testing.B, dir string, compression blob.Compression) *blob.Blob {
	b.Helper()

	indexData, dataData := createBenchArchive(b, dir, compression)
	base, err := blob.New(indexData, testutil.NewMockByteSource(dataData))
	if err != nil {
		b.Fatal(err)
	}

	return base
}

var _ cache.StreamingCache = (*disk.Cache)(nil)

func createBenchArchive(b *testing.B, dir string, compression blob.Compression) ([]byte, []byte) {
	b.Helper()

	var indexBuf, dataBuf bytes.Buffer
	var opts []blob.CreateOption
	if compression != blob.CompressionNone {
		opts = append(opts, blob.CreateWithCompression(compression))
	}
	if err := blob.Create(context.Background(), dir, &indexBuf, &dataBuf, opts...); err != nil {
		b.Fatal(err)
	}
	return indexBuf.Bytes(), dataBuf.Bytes()
}

func benchHTTPEnabled() bool {
	return os.Getenv("BLOB_BENCH_HTTP") != ""
}

func newBenchHTTPSource(b *testing.B, data []byte) (blob.ByteSource, func()) {
	b.Helper()

	cfg, err := benchHTTPConfigFromEnv()
	if err != nil {
		b.Fatal(err)
	}

	client := benchHTTPClient(cfg)
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		nethttp.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	}))

	src, err := blobhttp.NewSource(server.URL, blobhttp.WithClient(client))
	if err != nil {
		server.Close()
		b.Fatal(err)
	}

	return src, server.Close
}

type benchHTTPConfig struct {
	latency        time.Duration
	bytesPerSecond int64
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
