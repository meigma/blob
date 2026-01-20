package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	blob "github.com/meigma/blob/core"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var benchOCISink []byte

type benchPattern string

const (
	benchPatternCompressible benchPattern = "compressible"
	benchPatternRandom       benchPattern = "random"
)

type benchOCIData struct {
	blob     *blob.Blob
	readPath string
	dataSize int64
}

type memByteSource struct {
	data     []byte
	sourceID string
}

func newMemByteSource(data []byte) *memByteSource {
	return &memByteSource{
		data:     data,
		sourceID: fmt.Sprintf("mem:%d", len(data)),
	}
}

func (m *memByteSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}
	return n, nil
}

func (m *memByteSource) Size() int64 {
	return int64(len(m.data))
}

func (m *memByteSource) SourceID() string {
	return m.sourceID
}

type benchRefCache struct {
	mu   sync.RWMutex
	data map[string]string
}

func newBenchRefCache() *benchRefCache {
	return &benchRefCache{data: make(map[string]string)}
}

func (c *benchRefCache) GetDigest(ref string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.data[ref]
	return d, ok
}

func (c *benchRefCache) PutDigest(ref, dgst string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[ref] = dgst
	return nil
}

func (c *benchRefCache) Delete(ref string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, ref)
	return nil
}

func (c *benchRefCache) MaxBytes() int64            { return 0 }
func (c *benchRefCache) SizeBytes() int64           { return 0 }
func (c *benchRefCache) Prune(int64) (int64, error) { return 0, nil }

type benchManifestCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newBenchManifestCache() *benchManifestCache {
	return &benchManifestCache{data: make(map[string][]byte)}
}

func (c *benchManifestCache) GetManifest(dgst string) (*ocispec.Manifest, bool) {
	c.mu.RLock()
	raw, ok := c.data[dgst]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, false
	}
	return &manifest, true
}

func (c *benchManifestCache) PutManifest(dgst string, raw []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[dgst] = append([]byte(nil), raw...)
	return nil
}

func (c *benchManifestCache) Delete(dgst string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, dgst)
	return nil
}

func (c *benchManifestCache) MaxBytes() int64            { return 0 }
func (c *benchManifestCache) SizeBytes() int64           { return 0 }
func (c *benchManifestCache) Prune(int64) (int64, error) { return 0, nil }

type benchIndexCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newBenchIndexCache() *benchIndexCache {
	return &benchIndexCache{data: make(map[string][]byte)}
}

func (c *benchIndexCache) GetIndex(dgst string) ([]byte, bool) {
	c.mu.RLock()
	raw, ok := c.data[dgst]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return append([]byte(nil), raw...), true
}

func (c *benchIndexCache) PutIndex(dgst string, raw []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[dgst] = append([]byte(nil), raw...)
	return nil
}

func (c *benchIndexCache) Delete(dgst string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, dgst)
	return nil
}

func (c *benchIndexCache) MaxBytes() int64            { return 0 }
func (c *benchIndexCache) SizeBytes() int64           { return 0 }
func (c *benchIndexCache) Prune(int64) (int64, error) { return 0, nil }

func BenchmarkOCIFlow(b *testing.B) {
	if os.Getenv("BLOB_BENCH_OCI") != "1" {
		b.Skip("set BLOB_BENCH_OCI=1 to run OCI registry benchmarks")
	}

	ctx := context.Background()
	registry := startRegistry(ctx, b)
	c := New(WithPlainHTTP(true))

	cases := []struct {
		name        string
		fileCount   int
		fileSize    int
		compression blob.Compression
		pattern     benchPattern
	}{
		{
			name:        "files=64/size=16k/none/compressible",
			fileCount:   64,
			fileSize:    16 << 10,
			compression: blob.CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=256/size=64k/zstd/random",
			fileCount:   256,
			fileSize:    64 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			data := createBenchOCIData(b, bc.fileCount, bc.fileSize, bc.compression, bc.pattern)
			repo := "bench/" + sanitizeBenchName(bc.name)

			b.ReportAllocs()
			b.SetBytes(data.dataSize)
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				ref := fmt.Sprintf("%s/%s:run-%d", registry, repo, i)
				if err := c.Push(ctx, ref, data.blob); err != nil {
					b.Fatal(err)
				}

				pulled, err := c.Pull(ctx, ref, WithPullSkipCache())
				if err != nil {
					b.Fatal(err)
				}

				read, err := pulled.ReadFile(data.readPath)
				if err != nil {
					b.Fatal(err)
				}
				benchOCISink = read
			}
		})
	}
}

func BenchmarkOCIPullCache(b *testing.B) {
	if os.Getenv("BLOB_BENCH_OCI") != "1" {
		b.Skip("set BLOB_BENCH_OCI=1 to run OCI registry benchmarks")
	}

	ctx := context.Background()
	registry := startRegistry(ctx, b)
	pushClient := New(WithPlainHTTP(true))

	cases := []struct {
		name        string
		fileCount   int
		fileSize    int
		compression blob.Compression
		pattern     benchPattern
	}{
		{
			name:        "files=64/size=16k/none/compressible",
			fileCount:   64,
			fileSize:    16 << 10,
			compression: blob.CompressionNone,
			pattern:     benchPatternCompressible,
		},
		{
			name:        "files=256/size=64k/zstd/random",
			fileCount:   256,
			fileSize:    64 << 10,
			compression: blob.CompressionZstd,
			pattern:     benchPatternRandom,
		},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			data := createBenchOCIData(b, bc.fileCount, bc.fileSize, bc.compression, bc.pattern)
			repo := "bench-cache/" + sanitizeBenchName(bc.name)
			ref := fmt.Sprintf("%s/%s:baseline", registry, repo)
			if err := pushClient.Push(ctx, ref, data.blob); err != nil {
				b.Fatalf("push baseline: %v", err)
			}

			b.Run("cold", func(b *testing.B) {
				c := New(WithPlainHTTP(true))
				b.ReportAllocs()
				b.SetBytes(data.dataSize)
				b.ResetTimer()

				for b.Loop() {
					pulled, err := c.Pull(ctx, ref)
					if err != nil {
						b.Fatal(err)
					}
					read, err := pulled.ReadFile(data.readPath)
					if err != nil {
						b.Fatal(err)
					}
					benchOCISink = read
				}
			})

			b.Run("warm", func(b *testing.B) {
				refCache := newBenchRefCache()
				manifestCache := newBenchManifestCache()
				indexCache := newBenchIndexCache()
				c := New(
					WithPlainHTTP(true),
					WithRefCache(refCache),
					WithManifestCache(manifestCache),
					WithIndexCache(indexCache),
				)
				if _, err := c.Pull(ctx, ref); err != nil {
					b.Fatalf("warm pull: %v", err)
				}

				b.ReportAllocs()
				b.SetBytes(data.dataSize)
				b.ResetTimer()

				for b.Loop() {
					pulled, err := c.Pull(ctx, ref)
					if err != nil {
						b.Fatal(err)
					}
					read, err := pulled.ReadFile(data.readPath)
					if err != nil {
						b.Fatal(err)
					}
					benchOCISink = read
				}
			})
		})
	}
}

func startRegistry(ctx context.Context, tb testing.TB) string {
	tb.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "registry:2",
		ExposedPorts: []string{"5000/tcp"},
		WaitingFor:   wait.ForHTTP("/v2/").WithPort("5000/tcp").WithStatusCodeMatcher(isOKStatus),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		tb.Fatalf("start registry container: %v", err)
	}
	tb.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	host, err := container.Host(ctx)
	if err != nil {
		tb.Fatalf("resolve registry host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5000/tcp")
	if err != nil {
		tb.Fatalf("resolve registry port: %v", err)
	}

	return fmt.Sprintf("%s:%s", host, port.Port())
}

func isOKStatus(status int) bool {
	return status >= 200 && status < 300
}

func createBenchOCIData(tb testing.TB, fileCount, fileSize int, compression blob.Compression, pattern benchPattern) benchOCIData {
	tb.Helper()

	dir := tb.TempDir()
	readPath := writeBenchFiles(tb, dir, fileCount, fileSize, pattern)

	var indexBuf bytes.Buffer
	var dataBuf bytes.Buffer
	var opts []blob.CreateOption
	if compression != blob.CompressionNone {
		opts = append(opts, blob.CreateWithCompression(compression))
	}
	if err := blob.Create(context.Background(), dir, &indexBuf, &dataBuf, opts...); err != nil {
		tb.Fatalf("create archive: %v", err)
	}

	b, err := blob.New(indexBuf.Bytes(), newMemByteSource(dataBuf.Bytes()))
	if err != nil {
		tb.Fatalf("create blob: %v", err)
	}

	return benchOCIData{
		blob:     b,
		readPath: readPath,
		dataSize: int64(len(dataBuf.Bytes())),
	}
}

func writeBenchFiles(tb testing.TB, dir string, fileCount, fileSize int, pattern benchPattern) string {
	tb.Helper()

	rng := rand.New(rand.NewSource(1)) // deterministic data for repeatable runs
	var readPath string

	for i := range fileCount {
		subdir := fmt.Sprintf("dir%02d", i%16)
		name := fmt.Sprintf("file%05d.bin", i)
		rel := filepath.Join(subdir, name)
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			tb.Fatalf("mkdir: %v", err)
		}

		data := make([]byte, fileSize)
		switch pattern {
		case benchPatternRandom:
			if _, err := rng.Read(data); err != nil {
				tb.Fatalf("rand data: %v", err)
			}
		default:
			for j := range data {
				data[j] = 'a'
			}
		}

		if err := os.WriteFile(full, data, 0o644); err != nil {
			tb.Fatalf("write file: %v", err)
		}

		if i == 0 {
			readPath = filepath.ToSlash(rel)
		}
	}

	return readPath
}

func sanitizeBenchName(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "=", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return strings.Trim(name, "-")
}
