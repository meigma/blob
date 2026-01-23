package blob

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/containerd/stargz-snapshotter/estargz"
	blobcache "github.com/meigma/blob/core/cache"
	blobhttp "github.com/meigma/blob/core/http"
	"github.com/meigma/blob/core/internal/batch"
	"github.com/meigma/blob/core/internal/index"
	"github.com/meigma/blob/core/testutil"
)

type benchMetric struct {
	name  string
	value float64
}

func metric(name string, value float64) benchMetric {
	return benchMetric{name: name, value: value}
}

func reportMetrics(b *testing.B, metrics ...benchMetric) map[string]any {
	b.Helper()
	results := make(map[string]any, len(metrics))
	for _, m := range metrics {
		b.ReportMetric(m.value, m.name)
		results[m.name] = m.value
	}
	return results
}

func reportAndEmit(b *testing.B, params map[string]any, metrics ...benchMetric) {
	b.Helper()
	results := reportMetrics(b, metrics...)
	emitBenchJSON(b, params, results)
}

type benchJSONRecord struct {
	Benchmark string         `json:"benchmark"`
	Params    map[string]any `json:"params,omitempty"`
	Results   map[string]any `json:"results,omitempty"`
}

var benchJSONMu sync.Mutex

func emitBenchJSON(b *testing.B, params, results map[string]any) {
	b.Helper()

	if os.Getenv("BLOB_BENCH_JSON") == "" {
		return
	}

	record := benchJSONRecord{
		Benchmark: b.Name(),
		Params:    params,
		Results:   results,
	}
	data, err := json.Marshal(record)
	if err != nil {
		b.Logf("failed to marshal benchmark json: %v", err)
		return
	}

	benchJSONMu.Lock()
	defer benchJSONMu.Unlock()
	fmt.Fprintln(os.Stdout, string(data))
}

func benchLargeEnabled() bool {
	return os.Getenv("BLOB_BENCH_LARGE") != ""
}

func formatBytes(size int64) string {
	switch {
	case size%(1<<30) == 0:
		return fmt.Sprintf("%dGiB", size>>30)
	case size%(1<<20) == 0:
		return fmt.Sprintf("%dMiB", size>>20)
	case size%(1<<10) == 0:
		return fmt.Sprintf("%dKiB", size>>10)
	default:
		return fmt.Sprintf("%dB", size)
	}
}

func throughputMBs(totalBytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return (float64(totalBytes) / (1024 * 1024)) / elapsed.Seconds()
}

type benchByteSource interface {
	ByteSource
	Reset()
	BytesRead() int64
	RangeRequests() int64
}

type countingSource struct {
	src       ByteSource
	readBytes int64
	readCalls int64
}

func newCountingSource(src ByteSource) *countingSource {
	return &countingSource{src: src}
}

func (c *countingSource) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.src.ReadAt(p, off)
	if n > 0 {
		atomic.AddInt64(&c.readBytes, int64(n))
	}
	atomic.AddInt64(&c.readCalls, 1)
	return n, err
}

func (c *countingSource) Size() int64 {
	return c.src.Size()
}

func (c *countingSource) SourceID() string {
	return c.src.SourceID()
}

func (c *countingSource) Reset() {
	atomic.StoreInt64(&c.readBytes, 0)
	atomic.StoreInt64(&c.readCalls, 0)
}

func (c *countingSource) BytesRead() int64 {
	return atomic.LoadInt64(&c.readBytes)
}

func (c *countingSource) RangeRequests() int64 {
	return atomic.LoadInt64(&c.readCalls)
}

type rangeReader interface {
	ReadRange(off, length int64) (io.ReadCloser, error)
}

type countingRangeSource struct {
	*countingSource
	rr         rangeReader
	rangeBytes int64
	rangeCalls int64
}

func newCountingRangeSource(src ByteSource) *countingRangeSource {
	return &countingRangeSource{
		countingSource: newCountingSource(src),
		rr:             src.(rangeReader),
	}
}

func (c *countingRangeSource) ReadRange(off, length int64) (io.ReadCloser, error) {
	rc, err := c.rr.ReadRange(off, length)
	if err != nil {
		return nil, err
	}
	atomic.AddInt64(&c.rangeCalls, 1)
	return &countingReadCloser{rc: rc, onRead: func(n int) {
		atomic.AddInt64(&c.rangeBytes, int64(n))
	}}, nil
}

func (c *countingRangeSource) Reset() {
	c.countingSource.Reset()
	atomic.StoreInt64(&c.rangeBytes, 0)
	atomic.StoreInt64(&c.rangeCalls, 0)
}

func (c *countingRangeSource) BytesRead() int64 {
	return c.countingSource.BytesRead() + atomic.LoadInt64(&c.rangeBytes)
}

func (c *countingRangeSource) RangeRequests() int64 {
	return c.countingSource.RangeRequests() + atomic.LoadInt64(&c.rangeCalls)
}

type countingReadCloser struct {
	rc     io.ReadCloser
	onRead func(int)
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(n)
	}
	return n, err
}

func (c *countingReadCloser) Close() error {
	return c.rc.Close()
}

type countingReaderAt struct {
	ra        io.ReaderAt
	readBytes int64
	readCalls int64
}

func newCountingReaderAt(ra io.ReaderAt) *countingReaderAt {
	return &countingReaderAt{ra: ra}
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.ra.ReadAt(p, off)
	if n > 0 {
		atomic.AddInt64(&c.readBytes, int64(n))
	}
	atomic.AddInt64(&c.readCalls, 1)
	return n, err
}

func (c *countingReaderAt) Reset() {
	atomic.StoreInt64(&c.readBytes, 0)
	atomic.StoreInt64(&c.readCalls, 0)
}

func (c *countingReaderAt) BytesRead() int64 {
	return atomic.LoadInt64(&c.readBytes)
}

type countingReader struct {
	r     io.Reader
	bytes int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(&c.bytes, int64(n))
	}
	return n, err
}

func (c *countingReader) BytesRead() int64 {
	return atomic.LoadInt64(&c.bytes)
}

type countingCache struct {
	inner blobcache.Cache
	hits  int64
	gets  int64
}

func newCountingCache(inner blobcache.Cache) *countingCache {
	return &countingCache{inner: inner}
}

func (c *countingCache) Get(hash []byte) (fs.File, bool) {
	atomic.AddInt64(&c.gets, 1)
	f, ok := c.inner.Get(hash)
	if ok {
		atomic.AddInt64(&c.hits, 1)
	}
	return f, ok
}

func (c *countingCache) Put(hash []byte, f fs.File) error {
	return c.inner.Put(hash, f)
}

func (c *countingCache) Delete(hash []byte) error {
	return c.inner.Delete(hash)
}

func (c *countingCache) MaxBytes() int64 {
	return c.inner.MaxBytes()
}

func (c *countingCache) SizeBytes() int64 {
	return c.inner.SizeBytes()
}

func (c *countingCache) Prune(targetBytes int64) (int64, error) {
	return c.inner.Prune(targetBytes)
}

func (c *countingCache) Reset() {
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.gets, 0)
}

func (c *countingCache) Hits() int64 {
	return atomic.LoadInt64(&c.hits)
}

func (c *countingCache) Gets() int64 {
	return atomic.LoadInt64(&c.gets)
}

type discardSink struct{}

type discardCommitter struct{}

func (discardCommitter) Write(p []byte) (int, error) { return len(p), nil }
func (discardCommitter) Commit() error               { return nil }
func (discardCommitter) Discard() error              { return nil }

func (discardSink) ShouldProcess(*batch.Entry) bool {
	return true
}

func (discardSink) Writer(*batch.Entry) (batch.Committer, error) {
	return discardCommitter{}, nil
}

func (discardSink) PutBuffered(*batch.Entry, []byte) error {
	return nil
}

type httpMetrics struct {
	requestBytes  int64
	responseBytes int64
	requestCount  int64
}

type countingRoundTripper struct {
	base    http.RoundTripper
	metrics *httpMetrics
}

func (rt *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.metrics != nil {
		if dump, err := httputil.DumpRequestOut(req, false); err == nil {
			atomic.AddInt64(&rt.metrics.requestBytes, int64(len(dump)))
			atomic.AddInt64(&rt.metrics.requestCount, 1)
		}
	}

	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if rt.metrics != nil {
		if dump, err := httputil.DumpResponse(resp, false); err == nil {
			atomic.AddInt64(&rt.metrics.responseBytes, int64(len(dump)))
		}
	}

	return resp, nil
}

func newHTTPClientWithMetrics(cfg benchHTTPConfig, metrics *httpMetrics) *http.Client {
	transport := http.DefaultTransport
	if base, ok := transport.(*http.Transport); ok {
		transport = base.Clone()
	}
	if cfg.latency > 0 || cfg.bytesPerSecond > 0 {
		transport = &benchHTTPRoundTripper{
			base:           transport,
			latency:        cfg.latency,
			bytesPerSecond: cfg.bytesPerSecond,
		}
	}
	transport = &countingRoundTripper{base: transport, metrics: metrics}
	return &http.Client{Transport: transport}
}

//nolint:unparam // metrics return value available for future benchmarks
func newCountingHTTPSource(data []byte, cfg benchHTTPConfig) (benchByteSource, *httpMetrics, func(), error) {
	metrics := &httpMetrics{}
	client := newHTTPClientWithMetrics(cfg, metrics)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	}))

	src, err := blobhttp.NewSource(server.URL, blobhttp.WithClient(client))
	if err != nil {
		server.Close()
		return nil, nil, nil, err
	}

	cleanup := func() {
		server.Close()
	}

	return newCountingRangeSource(src), metrics, cleanup, nil
}

//nolint:unparam // fileSize parameter kept for flexibility
func buildSyntheticIndex(fileCount, fileSize int) []byte {
	entries := makeSyntheticEntries(fileCount, fileSize)
	return buildIndex(entries, uint64(fileCount*fileSize), nil)
}

func makeSyntheticEntries(fileCount, fileSize int) []Entry {
	paths := make([]string, 0, fileCount)
	for i := range fileCount {
		paths = append(paths, fmt.Sprintf("dir%03d/file%07d.dat", i/1000, i))
	}
	sort.Strings(paths)

	entries := make([]Entry, 0, fileCount)
	var offset uint64
	for _, path := range paths {
		hash := sha256.Sum256([]byte(path))
		entries = append(entries, Entry{
			Path:         path,
			DataOffset:   offset,
			DataSize:     uint64(fileSize),
			OriginalSize: uint64(fileSize),
			Hash:         hash[:],
			Mode:         0o644,
			ModTime:      time.Unix(0, 0),
		})
		offset += uint64(fileSize)
	}
	return entries
}

func buildTarFromDir(b *testing.B, dir string) []byte {
	b.Helper()

	var relPaths []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	if len(relPaths) == 0 {
		return nil
	}
	sort.Strings(relPaths)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, rel := range relPaths {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			b.Fatal(err)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			b.Fatal(err)
		}
		name := rel
		if info.IsDir() && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		hdr.Name = name
		hdr.ModTime = time.Unix(0, 0)
		hdr.AccessTime = time.Unix(0, 0)
		hdr.ChangeTime = time.Unix(0, 0)
		if err := tw.WriteHeader(hdr); err != nil {
			b.Fatal(err)
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(full)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				b.Fatal(err)
			}
			if err := f.Close(); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}

func buildZipFromDir(b *testing.B, dir string) []byte {
	b.Helper()

	var relPaths []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	if len(relPaths) == 0 {
		return nil
	}
	sort.Strings(relPaths)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range relPaths {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			b.Fatal(err)
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			b.Fatal(err)
		}
		hdr.Name = rel
		if info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			b.Fatal(err)
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(full)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := io.Copy(w, f); err != nil {
				f.Close()
				b.Fatal(err)
			}
			if err := f.Close(); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}

func readTarFile(data []byte, target string) (content []byte, bytesRead int64, err error) {
	counter := &countingReader{r: bytes.NewReader(data)}
	tr := tar.NewReader(counter)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				return nil, counter.BytesRead(), fmt.Errorf("tar: missing %s", target)
			}
			return nil, counter.BytesRead(), err
		}
		if strings.TrimSuffix(hdr.Name, "/") == target {
			content, err := io.ReadAll(tr)
			return content, counter.BytesRead(), err
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return nil, counter.BytesRead(), err
		}
	}
}

func makeNestedBenchFiles(b *testing.B, dir string, depth, fileCount, fileSize int) string {
	b.Helper()

	parts := make([]string, 0, depth)
	for i := range depth {
		parts = append(parts, fmt.Sprintf("level%02d", i))
	}
	prefix := filepath.Join(parts...)
	fullDir := filepath.Join(dir, prefix)
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		b.Fatal(err)
	}

	content := bytes.Repeat([]byte("a"), fileSize)
	for i := range fileCount {
		name := fmt.Sprintf("file%04d.dat", i)
		full := filepath.Join(fullDir, name)
		if err := os.WriteFile(full, content, 0o644); err != nil {
			b.Fatal(err)
		}
	}

	return filepath.ToSlash(prefix)
}

func filterPrefixPaths(paths []string, prefix string) []string {
	match := prefix + "/"
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.HasPrefix(path, match) {
			out = append(out, path)
		}
	}
	return out
}

func pickRandomPaths(paths []string, n int, seed int64) []string {
	if n <= 0 || len(paths) == 0 {
		return nil
	}
	if n > len(paths) {
		n = len(paths)
	}

	indices := make([]int, len(paths))
	for i := range indices {
		indices[i] = i
	}
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})
	selected := make([]string, 0, n)
	for i := range n {
		selected = append(selected, paths[indices[i]])
	}
	return selected
}

//nolint:unparam // opts parameter kept for flexibility
func newCountingBlob(indexData, dataData []byte, opts ...Option) (*Blob, benchByteSource, error) {
	src := testutil.NewMockByteSource(dataData)
	var counted benchByteSource
	if _, ok := interface{}(src).(rangeReader); ok {
		counted = newCountingRangeSource(src)
	} else {
		counted = newCountingSource(src)
	}
	blob, err := New(indexData, counted, opts...)
	if err != nil {
		return nil, nil, err
	}
	return blob, counted, nil
}

func BenchmarkSingleFileTransfer(b *testing.B) {
	const fileSize = 64 << 10
	archiveSizes := []int64{1 << 20, 10 << 20, 100 << 20, 1 << 30}

	for _, archiveSize := range archiveSizes {
		b.Run("archive="+formatBytes(archiveSize), func(b *testing.B) {
			if archiveSize >= 1<<30 && !benchLargeEnabled() {
				b.Skip("BLOB_BENCH_LARGE not set")
			}

			fileCount := int((archiveSize + fileSize - 1) / fileSize)
			if fileCount < 1 {
				fileCount = 1
			}

			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
			indexData, dataData := createBenchArchive(b, dir, CompressionNone)

			blob, source, err := newCountingBlob(indexData, dataData)
			if err != nil {
				b.Fatal(err)
			}

			path := paths[0]
			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				content, err := blob.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = content
			}
			elapsed := b.Elapsed()
			bytesTransferred := float64(source.BytesRead()) / float64(b.N)
			overhead := bytesTransferred - float64(fileSize)
			savings := 100 * (1 - bytesTransferred/float64(len(dataData)))

			params := map[string]any{
				"file_size_bytes":    fileSize,
				"archive_size_bytes": len(dataData),
			}
			reportAndEmit(b, params,
				metric("bytes_transferred", bytesTransferred),
				metric("file_size", float64(fileSize)),
				metric("archive_size", float64(len(dataData))),
				metric("overhead_bytes", overhead),
				metric("savings_percent", savings),
				metric("latency_ms", float64(elapsed.Milliseconds())/float64(b.N)),
			)
		})
	}
}

func BenchmarkMultiFileTransfer(b *testing.B) {
	const (
		archiveSize = 100 << 20
		fileSize    = 64 << 10
	)

	fileCount := int(archiveSize / fileSize)
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	cases := []int{1, 10, 50, 100}
	for _, filesToRead := range cases {
		pathsToRead := pickRandomPaths(paths, filesToRead, int64(filesToRead))
		name := fmt.Sprintf("files=%d", filesToRead)
		b.Run(name, func(b *testing.B) {
			blob, source, err := newCountingBlob(indexData, dataData)
			if err != nil {
				b.Fatal(err)
			}
			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for _, path := range pathsToRead {
					content, err := blob.ReadFile(path)
					if err != nil {
						b.Fatal(err)
					}
					benchSinkBytes = content
				}
			}

			bytesTransferred := float64(source.BytesRead()) / float64(b.N)
			totalFileSize := float64(filesToRead * fileSize)
			efficiency := 0.0
			if bytesTransferred > 0 {
				efficiency = 100 * totalFileSize / bytesTransferred
			}

			params := map[string]any{
				"files":               filesToRead,
				"file_size_bytes":     fileSize,
				"archive_size_bytes":  len(dataData),
				"total_file_size":     int(totalFileSize),
				"random_selection":    true,
				"files_per_iteration": filesToRead,
			}
			reportAndEmit(b, params,
				metric("bytes_transferred", bytesTransferred),
				metric("total_file_size", totalFileSize),
				metric("transfer_efficiency", efficiency),
			)
		})
	}
}

func BenchmarkSequentialVsRandom(b *testing.B) {
	const (
		fileCount = 1024
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	sort.Strings(paths)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	adjacent := append([]string(nil), paths[:10]...)
	randomPaths := pickRandomPaths(paths, 10, 42)

	cases := []struct {
		name  string
		paths []string
	}{
		{name: "adjacent", paths: adjacent},
		{name: "random", paths: randomPaths},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			blob, source, err := newCountingBlob(indexData, dataData)
			if err != nil {
				b.Fatal(err)
			}

			entries := blob.collectPathEntries(bc.paths)
			processor := batch.NewProcessor(blob.reader.Source(), blob.reader.Pool(), blob.maxFileSize, batch.WithReadConcurrency(1))

			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := processor.Process(entries, discardSink{}); err != nil {
					b.Fatal(err)
				}
			}

			bytesTransferred := float64(source.BytesRead()) / float64(b.N)
			rangeRequests := float64(source.RangeRequests()) / float64(b.N)
			params := map[string]any{
				"files":         len(bc.paths),
				"archive_files": fileCount,
			}
			reportAndEmit(b, params,
				metric("bytes_transferred", bytesTransferred),
				metric("range_request_count", rangeRequests),
			)
		})
	}
}

func BenchmarkDirectoryTransfer(b *testing.B) {
	const (
		fileCount = 512
		fileSize  = 16 << 10
		prefix    = "dir00"
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	prefixPaths := filterPrefixPaths(paths, prefix)
	if len(prefixPaths) == 0 {
		b.Fatal("expected prefix files")
	}

	cases := []struct {
		name string
		fn   func(*Blob, benchByteSource) error
	}{
		{
			name: "single-range",
			fn: func(blob *Blob, _ benchByteSource) error {
				entries := blob.collectPrefixEntries(prefix)
				processor := batch.NewProcessor(blob.reader.Source(), blob.reader.Pool(), blob.maxFileSize, batch.WithReadConcurrency(1))
				_, err := processor.Process(entries, discardSink{})
				return err
			},
		},
		{
			name: "per-file",
			fn: func(blob *Blob, _ benchByteSource) error {
				for _, path := range prefixPaths {
					content, err := blob.ReadFile(path)
					if err != nil {
						return err
					}
					benchSinkBytes = content
				}
				return nil
			},
		},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			blob, source, err := newCountingBlob(indexData, dataData)
			if err != nil {
				b.Fatal(err)
			}

			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := bc.fn(blob, source); err != nil {
					b.Fatal(err)
				}
			}

			bytesTransferred := float64(source.BytesRead()) / float64(b.N)
			rangeRequests := float64(source.RangeRequests()) / float64(b.N)
			params := map[string]any{
				"prefix":     prefix,
				"file_count": len(prefixPaths),
			}
			reportAndEmit(b, params,
				metric("bytes_transferred", bytesTransferred),
				metric("range_request_count", rangeRequests),
			)
		})
	}
}

func BenchmarkIndexSize(b *testing.B) {
	cases := []int{100, 1000, 10000, 100000}
	const fileSize = 4 << 10

	for _, fileCount := range cases {
		name := fmt.Sprintf("files=%d", fileCount)
		b.Run(name, func(b *testing.B) {
			indexData := buildSyntheticIndex(fileCount, fileSize)
			indexBytes := len(indexData)
			bytesPerEntry := float64(indexBytes) / float64(fileCount)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchSinkInt = indexBytes
			}

			params := map[string]any{
				"file_count": fileCount,
			}
			reportAndEmit(b, params,
				metric("index_bytes", float64(indexBytes)),
				metric("file_count", float64(fileCount)),
				metric("bytes_per_entry", bytesPerEntry),
			)
		})
	}
}

func BenchmarkIndexParse(b *testing.B) {
	cases := []int{100, 1000, 10000, 100000}
	const fileSize = 4 << 10

	for _, fileCount := range cases {
		name := fmt.Sprintf("files=%d", fileCount)
		b.Run(name, func(b *testing.B) {
			indexData := buildSyntheticIndex(fileCount, fileSize)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				idx, err := index.Load(indexData)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkInt = idx.Len()
			}

			latency := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			params := map[string]any{
				"file_count": fileCount,
			}
			reportAndEmit(b, params, metric("parse_latency_ns", latency))
		})
	}
}

func BenchmarkFileLookup(b *testing.B) {
	cases := []int{100, 1000, 10000, 100000}
	const fileSize = 4 << 10

	for _, fileCount := range cases {
		name := fmt.Sprintf("files=%d", fileCount)
		b.Run(name, func(b *testing.B) {
			indexData := buildSyntheticIndex(fileCount, fileSize)
			idx, err := index.Load(indexData)
			if err != nil {
				b.Fatal(err)
			}
			paths := makeSyntheticEntries(fileCount, fileSize)
			path := paths[len(paths)/2].Path

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				view, ok := idx.LookupView(path)
				if !ok {
					b.Fatalf("missing %s", path)
				}
				benchSinkView = view
			}

			latency := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			params := map[string]any{
				"file_count": fileCount,
			}
			reportAndEmit(b, params,
				metric("lookup_latency_ns", latency),
				metric("file_count", float64(fileCount)),
			)
		})
	}
}

func BenchmarkIndexFetchHTTP(b *testing.B) {
	const fileCount = 10000
	const fileSize = 4 << 10

	indexData := buildSyntheticIndex(fileCount, fileSize)
	cfg := benchHTTPConfig{latency: 5 * time.Millisecond}
	metrics := &httpMetrics{}
	client := newHTTPClientWithMetrics(cfg, metrics)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(indexData)
	}))
	defer server.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = body
	}

	fetchLatency := float64(b.Elapsed().Milliseconds()) / float64(b.N)
	params := map[string]any{
		"latency_ms": 5,
		"file_count": fileCount,
	}
	reportAndEmit(b, params,
		metric("fetch_latency_ms", fetchLatency),
		metric("index_bytes", float64(len(indexData))),
	)
}

func BenchmarkReadDir(b *testing.B) {
	cases := []int{10, 100, 1000}
	const fileSize = 4 << 10

	for _, entries := range cases {
		fileCount := entries * benchDirCount
		name := fmt.Sprintf("entries=%d", entries)
		b.Run(name, func(b *testing.B) {
			dir := b.TempDir()
			makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
			blob := createBenchBlob(b, dir, CompressionNone)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				dirs, err := blob.ReadDir("dir00")
				if err != nil {
					b.Fatal(err)
				}
				benchSinkDirs = dirs
			}

			latency := float64(b.Elapsed().Microseconds()) / float64(b.N)
			params := map[string]any{
				"entry_count": entries,
			}
			reportAndEmit(b, params,
				metric("latency_us", latency),
				metric("entry_count", float64(entries)),
			)
		})
	}
}

func BenchmarkReadDirNested(b *testing.B) {
	const (
		depth     = 5
		fileCount = 100
		fileSize  = 4 << 10
	)

	dir := b.TempDir()
	prefix := makeNestedBenchFiles(b, dir, depth, fileCount, fileSize)
	blob := createBenchBlob(b, dir, CompressionNone)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dirs, err := blob.ReadDir(prefix)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkDirs = dirs
	}

	latency := float64(b.Elapsed().Microseconds()) / float64(b.N)
	params := map[string]any{
		"depth":       depth,
		"entry_count": fileCount,
	}
	reportAndEmit(b, params, metric("latency_us", latency))
}

func BenchmarkCopyDirContiguous(b *testing.B) {
	const (
		fileCount = 512
		fileSize  = 16 << 10
		prefix    = "dir00"
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	blob, source, err := newCountingBlob(indexData, dataData)
	if err != nil {
		b.Fatal(err)
	}

	entries := blob.collectPrefixEntries(prefix)
	processor := batch.NewProcessor(blob.reader.Source(), blob.reader.Pool(), blob.maxFileSize, batch.WithReadConcurrency(1))
	bytesPerOp := int64(len(entries) * fileSize)

	source.Reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := processor.Process(entries, discardSink{}); err != nil {
			b.Fatal(err)
		}
	}

	throughput := throughputMBs(bytesPerOp*int64(b.N), b.Elapsed())
	requests := float64(source.RangeRequests()) / float64(b.N)
	params := map[string]any{
		"prefix": prefix,
	}
	reportAndEmit(b, params,
		metric("throughput_mb_s", throughput),
		metric("range_request_count", requests),
	)
}

func BenchmarkCopyDirVsIndividual(b *testing.B) {
	const (
		fileCount = 256
		fileSize  = 16 << 10
		prefix    = "assets"
	)

	dir := b.TempDir()
	assetsDir := filepath.Join(dir, prefix)
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		b.Fatal(err)
	}
	content := bytes.Repeat([]byte("a"), fileSize)
	paths := make([]string, 0, fileCount)
	for i := range fileCount {
		path := filepath.Join(assetsDir, fmt.Sprintf("file%04d.dat", i))
		if err := os.WriteFile(path, content, 0o644); err != nil {
			b.Fatal(err)
		}
		paths = append(paths, filepath.ToSlash(filepath.Join(prefix, filepath.Base(path))))
	}

	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	cases := []struct {
		name string
		fn   func(*Blob) error
	}{
		{
			name: "copydir",
			fn: func(blob *Blob) error {
				entries := blob.collectPrefixEntries(prefix)
				processor := batch.NewProcessor(blob.reader.Source(), blob.reader.Pool(), blob.maxFileSize, batch.WithReadConcurrency(1))
				_, err := processor.Process(entries, discardSink{})
				return err
			},
		},
		{
			name: "individual",
			fn: func(blob *Blob) error {
				for _, path := range paths {
					content, err := blob.ReadFile(path)
					if err != nil {
						return err
					}
					benchSinkBytes = content
				}
				return nil
			},
		},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			blob, source, err := newCountingBlob(indexData, dataData)
			if err != nil {
				b.Fatal(err)
			}

			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := bc.fn(blob); err != nil {
					b.Fatal(err)
				}
			}

			totalLatency := float64(b.Elapsed().Milliseconds()) / float64(b.N)
			bytesTransferred := float64(source.BytesRead()) / float64(b.N)
			rangeRequests := float64(source.RangeRequests()) / float64(b.N)

			params := map[string]any{
				"prefix":     prefix,
				"file_count": fileCount,
			}
			reportAndEmit(b, params,
				metric("total_latency_ms", totalLatency),
				metric("bytes_transferred", bytesTransferred),
				metric("range_request_count", rangeRequests),
			)
		})
	}
}

func BenchmarkCacheMiss(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	cfg := benchHTTPConfig{latency: 5 * time.Millisecond}

	source, _, cleanup, err := newCountingHTTPSource(dataData, cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer cleanup()

	path := paths[0]
	var totalBytes int64

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		source.Reset()
		cache := newCountingCache(testutil.NewMockCache())
		cached, err := New(indexData, source, WithCache(cache))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		content, err := cached.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		b.StopTimer()
		totalBytes += source.BytesRead()
		b.StartTimer()
	}

	latency := float64(b.Elapsed().Milliseconds()) / float64(b.N)
	bytesTransferred := float64(totalBytes) / float64(b.N)
	params := map[string]any{
		"cache_state": "cold",
		"source":      "http",
	}
	reportAndEmit(b, params,
		metric("latency_ms", latency),
		metric("bytes_transferred", bytesTransferred),
	)
}

func BenchmarkCacheHit(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	cfg := benchHTTPConfig{latency: 5 * time.Millisecond}

	source, _, cleanup, err := newCountingHTTPSource(dataData, cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer cleanup()

	cache := newCountingCache(testutil.NewMockCache())
	cached, err := New(indexData, source, WithCache(cache))
	if err != nil {
		b.Fatal(err)
	}

	path := paths[0]
	content, err := cached.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	benchSinkBytes = content
	source.Reset()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		content, err := cached.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
	}

	latency := float64(b.Elapsed().Microseconds()) / float64(b.N)
	bytesTransferred := float64(source.BytesRead()) / float64(b.N)
	params := map[string]any{
		"cache_state": "warm",
		"source":      "http",
	}
	reportAndEmit(b, params,
		metric("latency_us", latency),
		metric("bytes_transferred", bytesTransferred),
	)
}

func BenchmarkCacheDeduplication(b *testing.B) {
	const fileSize = 64 << 10

	firstDir := b.TempDir()
	secondDir := b.TempDir()
	paths := makeBenchFiles(b, firstDir, 1, fileSize, benchPatternCompressible)
	makeBenchFiles(b, secondDir, 1, fileSize, benchPatternCompressible)

	indexA, dataA := createBenchArchive(b, firstDir, CompressionZstd)
	indexB, dataB := createBenchArchive(b, secondDir, CompressionZstd)

	sourceA := newCountingSource(testutil.NewMockByteSource(dataA))
	sourceB := newCountingSource(testutil.NewMockByteSource(dataB))

	path := paths[0]
	var totalBytes int64
	var totalHits int64

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		sourceA.Reset()
		sourceB.Reset()
		cache := newCountingCache(testutil.NewMockCache())
		blobA, err := New(indexA, sourceA, WithCache(cache))
		if err != nil {
			b.Fatal(err)
		}
		blobB, err := New(indexB, sourceB, WithCache(cache))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		content, err := blobA.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		content, err = blobB.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content

		b.StopTimer()
		totalBytes += sourceA.BytesRead() + sourceB.BytesRead()
		totalHits += cache.Hits()
		b.StartTimer()
	}

	bytesTransferred := float64(totalBytes) / float64(b.N)
	params := map[string]any{
		"archives": 2,
	}
	reportAndEmit(b, params,
		metric("cache_hits", float64(totalHits)/float64(b.N)),
		metric("bytes_transferred", bytesTransferred),
	)
}

func BenchmarkCacheHitRatio(b *testing.B) {
	const (
		fileCount    = 100
		fileSize     = 16 << 10
		duplicatePct = 30
	)

	dir := b.TempDir()
	uniqueCount := fileCount - (fileCount*duplicatePct)/100
	paths := makeBenchFiles(b, dir, uniqueCount, fileSize, benchPatternCompressible)
	content := bytes.Repeat([]byte("x"), fileSize)
	for i := uniqueCount; i < fileCount; i++ {
		path := fmt.Sprintf("dup/file%04d.dat", i)
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			b.Fatal(err)
		}
		paths = append(paths, path)
	}

	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	source := newCountingSource(testutil.NewMockByteSource(dataData))
	var totalHits int64
	var totalGets int64
	var totalBytes int64
	var totalSaved float64

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		source.Reset()
		cache := newCountingCache(testutil.NewMockCache())
		blob, err := New(indexData, source, WithCache(cache))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		for _, path := range paths {
			content, err := blob.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			benchSinkBytes = content
		}

		b.StopTimer()
		totalHits += cache.Hits()
		totalGets += cache.Gets()
		bytesTransferred := float64(source.BytesRead())
		bytesExpected := float64(len(paths) * fileSize)
		totalBytes += int64(bytesTransferred)
		totalSaved += bytesExpected - bytesTransferred
		b.StartTimer()
	}

	hitRatio := 0.0
	if totalGets > 0 {
		hitRatio = 100 * float64(totalHits) / float64(totalGets)
	}
	bytesSaved := totalSaved / float64(b.N)
	params := map[string]any{
		"file_count":        fileCount,
		"duplicate_percent": duplicatePct,
	}
	reportAndEmit(b, params,
		metric("hit_ratio", hitRatio),
		metric("bytes_saved", bytesSaved),
	)
}

func BenchmarkIndexCacheBenefit(b *testing.B) {
	const (
		fileCount = 10000
		fileSize  = 4 << 10
	)

	indexData := buildSyntheticIndex(fileCount, fileSize)
	cfg := benchHTTPConfig{latency: 5 * time.Millisecond}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(indexData)
	}))
	defer server.Close()

	client := benchHTTPClient(cfg)
	var coldTotal time.Duration
	var warmTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
		if reqErr != nil {
			b.Fatal(reqErr)
		}
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := index.Load(body); err != nil {
			b.Fatal(err)
		}
		coldTotal += time.Since(start)

		start = time.Now()
		if _, err := index.Load(indexData); err != nil {
			b.Fatal(err)
		}
		warmTotal += time.Since(start)
	}

	params := map[string]any{
		"file_count": fileCount,
		"latency_ms": 5,
	}
	reportAndEmit(b, params,
		metric("first_open_latency_ms", float64(coldTotal.Milliseconds())/float64(b.N)),
		metric("second_open_latency_us", float64(warmTotal.Microseconds())/float64(b.N)),
	)
}

func BenchmarkLatencyImpact(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	latencies := []time.Duration{1 * time.Millisecond, 5 * time.Millisecond, 20 * time.Millisecond, 100 * time.Millisecond}
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	path := paths[0]

	for _, latency := range latencies {
		name := fmt.Sprintf("latency=%s", latency)
		b.Run(name, func(b *testing.B) {
			source, _, cleanup, err := newCountingHTTPSource(dataData, benchHTTPConfig{latency: latency})
			if err != nil {
				b.Fatal(err)
			}
			defer cleanup()

			blob, err := New(indexData, source)
			if err != nil {
				b.Fatal(err)
			}

			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				content, err := blob.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = content
			}

			totalLatency := float64(b.Elapsed().Milliseconds()) / float64(b.N)
			rangeRequests := float64(source.RangeRequests()) / float64(b.N)
			rttMs := float64(latency.Milliseconds())
			dataTransfer := totalLatency - (rttMs * rangeRequests)
			if dataTransfer < 0 {
				dataTransfer = 0
			}

			params := map[string]any{
				"rtt_ms": latency.Milliseconds(),
			}
			reportAndEmit(b, params,
				metric("total_latency_ms", totalLatency),
				metric("rtt", rttMs),
				metric("data_transfer_time", dataTransfer),
			)
		})
	}
}

func BenchmarkBandwidthImpact(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	bpsCases := []int64{1_000_000 / 8, 10_000_000 / 8, 100_000_000 / 8, 1_000_000_000 / 8}
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	path := paths[0]

	for _, bps := range bpsCases {
		name := fmt.Sprintf("bps=%d", bps)
		b.Run(name, func(b *testing.B) {
			source, _, cleanup, err := newCountingHTTPSource(dataData, benchHTTPConfig{bytesPerSecond: bps})
			if err != nil {
				b.Fatal(err)
			}
			defer cleanup()

			blob, err := New(indexData, source)
			if err != nil {
				b.Fatal(err)
			}

			source.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				content, err := blob.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = content
			}

			throughput := throughputMBs(int64(fileSize*b.N), b.Elapsed())
			maxThroughput := float64(bps) / (1024 * 1024)
			utilization := 0.0
			if maxThroughput > 0 {
				utilization = 100 * throughput / maxThroughput
			}

			params := map[string]any{
				"bandwidth_bps": bps,
			}
			reportAndEmit(b, params,
				metric("throughput_mb_s", throughput),
				metric("bandwidth_utilization", utilization),
			)
		})
	}
}

func BenchmarkRangeRequestOverhead(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)
	path := paths[0]

	metrics := &httpMetrics{}
	client := newHTTPClientWithMetrics(benchHTTPConfig{latency: 0}, metrics)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(dataData))
	}))
	defer server.Close()

	src, err := blobhttp.NewSource(server.URL, blobhttp.WithClient(client))
	if err != nil {
		b.Fatal(err)
	}

	counted := newCountingRangeSource(src)
	blob, err := New(indexData, counted)
	if err != nil {
		b.Fatal(err)
	}

	counted.Reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		content, err := blob.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
	}

	requests := float64(atomic.LoadInt64(&metrics.requestCount)) / float64(b.N)
	requestOverhead := float64(atomic.LoadInt64(&metrics.requestBytes)) / float64(b.N)
	responseOverhead := float64(atomic.LoadInt64(&metrics.responseBytes)) / float64(b.N)
	if requests > 0 {
		requestOverhead /= requests
		responseOverhead /= requests
	}
	params := map[string]any{
		"range_requests": requests,
	}
	reportAndEmit(b, params,
		metric("request_overhead_bytes", requestOverhead),
		metric("response_overhead_bytes", responseOverhead),
	)
}

func BenchmarkConcurrentReads(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	selection := pickRandomPaths(paths, 10, 99)

	source, _, cleanup, err := newCountingHTTPSource(dataData, benchHTTPConfig{latency: 5 * time.Millisecond})
	if err != nil {
		b.Fatal(err)
	}
	defer cleanup()

	blob, err := New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	var sequentialTotal time.Duration
	var concurrentTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		for _, path := range selection {
			content, err := blob.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			benchSinkBytes = content
		}
		sequentialTotal += time.Since(start)

		start = time.Now()
		errCh := make(chan error, len(selection))
		var wg sync.WaitGroup
		for _, path := range selection {
			wg.Add(1)
			go func() {
				defer wg.Done()
				content, err := blob.ReadFile(path)
				if err != nil {
					errCh <- err
					return
				}
				benchSinkBytes = content
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				b.Fatal(err)
			}
		}
		concurrentTotal += time.Since(start)
	}

	seqMs := float64(sequentialTotal.Milliseconds()) / float64(b.N)
	concMs := float64(concurrentTotal.Milliseconds()) / float64(b.N)
	benefit := 0.0
	if concMs > 0 {
		benefit = seqMs / concMs
	}
	params := map[string]any{
		"files": len(selection),
	}
	reportAndEmit(b, params,
		metric("total_latency_ms", seqMs),
		metric("parallelism_benefit", benefit),
	)
}

func BenchmarkCompressionRatio(b *testing.B) {
	const (
		fileCount = 256
		fileSize  = 16 << 10
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)

	indexNone, dataNone := createBenchArchive(b, dir, CompressionNone)
	_, _ = indexNone, dataNone
	indexZstd, dataZstd := createBenchArchive(b, dir, CompressionZstd)
	_, _ = indexZstd, dataZstd

	uncompressed := float64(len(dataNone))
	compressed := float64(len(dataZstd))
	ratio := 0.0
	if uncompressed > 0 {
		ratio = compressed / uncompressed
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchSinkInt = len(dataZstd)
	}

	params := map[string]any{
		"file_count": fileCount,
		"pattern":    "compressible",
	}
	reportAndEmit(b, params,
		metric("uncompressed_size", uncompressed),
		metric("compressed_size", compressed),
		metric("ratio", ratio),
	)
}

func BenchmarkCompressionThroughput(b *testing.B) {
	const (
		fileCount = 512
		fileSize  = 16 << 10
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	totalBytes := int64(fileCount * fileSize)

	cases := []struct {
		name        string
		compression Compression
	}{
		{name: "none", compression: CompressionNone},
		{name: "zstd", compression: CompressionZstd},
	}

	for _, bc := range cases {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var indexBuf, dataBuf bytes.Buffer
				var opts []CreateOption
				if bc.compression != CompressionNone {
					opts = append(opts, CreateWithCompression(bc.compression))
				}
				if err := Create(context.Background(), dir, &indexBuf, &dataBuf, opts...); err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = dataBuf.Bytes()
			}

			throughput := throughputMBs(totalBytes*int64(b.N), b.Elapsed())
			params := map[string]any{
				"compression": bc.name,
			}
			reportAndEmit(b, params, metric("throughput_mb_s", throughput))
		})
	}
}

func BenchmarkDecompressionOverhead(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexNone, dataNone := createBenchArchive(b, dir, CompressionNone)
	indexZstd, dataZstd := createBenchArchive(b, dir, CompressionZstd)

	blobNone, err := New(indexNone, testutil.NewMockByteSource(dataNone))
	if err != nil {
		b.Fatal(err)
	}
	blobZstd, err := New(indexZstd, testutil.NewMockByteSource(dataZstd))
	if err != nil {
		b.Fatal(err)
	}

	path := paths[0]
	var noneTotal time.Duration
	var zstdTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		content, err := blobNone.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		noneTotal += time.Since(start)

		start = time.Now()
		content, err = blobZstd.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		zstdTotal += time.Since(start)
	}

	noneLatency := float64(noneTotal.Microseconds()) / float64(b.N)
	zstdLatency := float64(zstdTotal.Microseconds()) / float64(b.N)
	overhead := 0.0
	if noneLatency > 0 {
		overhead = 100 * ((zstdLatency / noneLatency) - 1)
	}

	params := map[string]any{
		"file_size": fileSize,
	}
	reportAndEmit(b, params,
		metric("read_latency_none", noneLatency),
		metric("read_latency_zstd", zstdLatency),
		metric("overhead", overhead),
	)
}

func BenchmarkPerFileIndependence(b *testing.B) {
	const (
		fileCount = 128
		fileSize  = 64 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)

	source := newCountingSource(testutil.NewMockByteSource(dataData))
	blob, err := New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	path := paths[len(paths)-1]
	source.Reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		content, err := blob.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
	}

	bytesRead := float64(source.BytesRead()) / float64(b.N)
	params := map[string]any{
		"file": path,
	}
	reportAndEmit(b, params,
		metric("bytes_read", bytesRead),
		metric("files_decompressed", 1),
	)
}

func BenchmarkVsTarFullDownload(b *testing.B) {
	const (
		archiveSize = 100 << 20
		fileSize    = 64 << 10
	)

	fileCount := int(archiveSize / fileSize)
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)
	tarData := buildTarFromDir(b, dir)

	path := paths[len(paths)-1]

	source := newCountingSource(testutil.NewMockByteSource(dataData))
	blob, err := New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	var blobBytes int64
	var tarBytes int64

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		source.Reset()
		content, err := blob.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		blobBytes += source.BytesRead()

		content, bytesRead, err := readTarFile(tarData, path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		tarBytes += bytesRead
	}

	params := map[string]any{
		"archive_size": len(dataData),
	}
	reportAndEmit(b, params,
		metric("bytes_transferred_blob", float64(blobBytes)/float64(b.N)),
		metric("bytes_transferred_tar", float64(tarBytes)/float64(b.N)),
	)
}

func BenchmarkVsTarExtractOne(b *testing.B) {
	const (
		archiveSize = 100 << 20
		fileSize    = 64 << 10
	)

	fileCount := int(archiveSize / fileSize)
	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)
	tarData := buildTarFromDir(b, dir)
	path := paths[len(paths)-1]

	source := newCountingSource(testutil.NewMockByteSource(dataData))
	blob, err := New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	var blobTotal time.Duration
	var tarTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		content, err := blob.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		blobTotal += time.Since(start)

		start = time.Now()
		content, _, err = readTarFile(tarData, path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		tarTotal += time.Since(start)
	}

	params := map[string]any{
		"archive_size": len(dataData),
	}
	reportAndEmit(b, params,
		metric("latency_blob", float64(blobTotal.Milliseconds())/float64(b.N)),
		metric("latency_tar", float64(tarTotal.Milliseconds())/float64(b.N)),
	)
}

func BenchmarkVsEstargz(b *testing.B) {
	const (
		fileCount = 128
		fileSize  = 16 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionZstd)
	target := paths[len(paths)/2]

	tarData := buildTarFromDir(b, dir)
	sr := io.NewSectionReader(bytes.NewReader(tarData), 0, int64(len(tarData)))
	rc, err := estargz.Build(sr)
	if err != nil {
		b.Fatal(err)
	}
	var estargzBuf bytes.Buffer
	if _, err = io.Copy(&estargzBuf, rc); err != nil {
		rc.Close()
		b.Fatal(err)
	}
	if err = rc.Close(); err != nil {
		b.Fatal(err)
	}
	zdata := estargzBuf.Bytes()

	countedEstar := newCountingReaderAt(bytes.NewReader(zdata))
	esr := io.NewSectionReader(countedEstar, 0, int64(len(zdata)))
	er, err := estargz.Open(esr)
	if err != nil {
		b.Fatal(err)
	}

	source := newCountingSource(testutil.NewMockByteSource(dataData))
	blob, err := New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	var blobTotal time.Duration
	var estargzTotal time.Duration
	var blobBytes int64
	var estargzBytes int64

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		source.Reset()
		start := time.Now()
		content, err := blob.ReadFile(target)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		blobTotal += time.Since(start)
		blobBytes += source.BytesRead()

		countedEstar.Reset()
		start = time.Now()
		fileReader, err := er.OpenFile(target)
		if err != nil {
			b.Fatal(err)
		}
		content, err = io.ReadAll(fileReader)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		estargzTotal += time.Since(start)
		estargzBytes += countedEstar.BytesRead()
	}

	params := map[string]any{
		"file": target,
	}
	reportAndEmit(b, params,
		metric("latency", float64(blobTotal.Milliseconds())/float64(b.N)),
		metric("bytes_transferred", float64(blobBytes)/float64(b.N)),
		metric("estargz_latency", float64(estargzTotal.Milliseconds())/float64(b.N)),
		metric("estargz_bytes_transferred", float64(estargzBytes)/float64(b.N)),
	)
}

func BenchmarkVsZip(b *testing.B) {
	const (
		fileCount = 128
		fileSize  = 16 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)
	zipData := buildZipFromDir(b, dir)
	path := paths[len(paths)/2]

	countedZip := newCountingReaderAt(bytes.NewReader(zipData))
	zr, err := zip.NewReader(countedZip, int64(len(zipData)))
	if err != nil {
		b.Fatal(err)
	}

	source := newCountingSource(testutil.NewMockByteSource(dataData))
	blob, err := New(indexData, source)
	if err != nil {
		b.Fatal(err)
	}

	var blobTotal time.Duration
	var zipTotal time.Duration
	var blobBytes int64
	var zipBytes int64

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		source.Reset()
		start := time.Now()
		content, err := blob.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		blobTotal += time.Since(start)
		blobBytes += source.BytesRead()

		countedZip.Reset()
		start = time.Now()
		zf, err := zr.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		content, err = io.ReadAll(zf)
		zf.Close()
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		zipTotal += time.Since(start)
		zipBytes += countedZip.BytesRead()
	}

	params := map[string]any{
		"file": path,
	}
	reportAndEmit(b, params,
		metric("latency", float64(blobTotal.Milliseconds())/float64(b.N)),
		metric("bytes_transferred", float64(blobBytes)/float64(b.N)),
		metric("zip_latency", float64(zipTotal.Milliseconds())/float64(b.N)),
		metric("zip_bytes_transferred", float64(zipBytes)/float64(b.N)),
	)
}

func BenchmarkCreateSmall(b *testing.B) {
	benchmarkCreateSize(b, "small", 100, 16<<10)
}

func BenchmarkCreateMedium(b *testing.B) {
	benchmarkCreateSize(b, "medium", 1000, 64<<10)
}

func BenchmarkCreateLarge(b *testing.B) {
	if !benchLargeEnabled() {
		b.Skip("BLOB_BENCH_LARGE not set")
	}
	benchmarkCreateSize(b, "large", 10000, 64<<10)
}

func benchmarkCreateSize(b *testing.B, label string, fileCount, fileSize int) {
	b.Helper()

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	totalBytes := int64(fileCount * fileSize)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var indexBuf, dataBuf bytes.Buffer
		if err := Create(context.Background(), dir, &indexBuf, &dataBuf); err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = dataBuf.Bytes()
	}

	throughput := throughputMBs(totalBytes*int64(b.N), b.Elapsed())
	latency := float64(b.Elapsed().Milliseconds()) / float64(b.N)
	params := map[string]any{
		"size": label,
	}
	reportAndEmit(b, params,
		metric("throughput_mb_s", throughput),
		metric("latency_ms", latency),
	)
}

func BenchmarkCreateWithCompression(b *testing.B) {
	const (
		fileCount = 512
		fileSize  = 16 << 10
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	totalBytes := int64(fileCount * fileSize)

	var noneTotal time.Duration
	var zstdTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		{
			var indexBuf, dataBuf bytes.Buffer
			if err := Create(context.Background(), dir, &indexBuf, &dataBuf); err != nil {
				b.Fatal(err)
			}
			benchSinkBytes = dataBuf.Bytes()
		}
		noneTotal += time.Since(start)

		start = time.Now()
		{
			var indexBuf, dataBuf bytes.Buffer
			if err := Create(context.Background(), dir, &indexBuf, &dataBuf, CreateWithCompression(CompressionZstd)); err != nil {
				b.Fatal(err)
			}
			benchSinkBytes = dataBuf.Bytes()
		}
		zstdTotal += time.Since(start)
	}

	throughputNone := throughputMBs(totalBytes*int64(b.N), noneTotal)
	throughputZstd := throughputMBs(totalBytes*int64(b.N), zstdTotal)
	params := map[string]any{
		"file_count": fileCount,
	}
	reportAndEmit(b, params,
		metric("throughput_none", throughputNone),
		metric("throughput_zstd", throughputZstd),
	)
}

func BenchmarkScaleFileCount(b *testing.B) {
	cases := []int{1000, 10000, 100000, 1000000}
	const fileSize = 4 << 10

	for _, fileCount := range cases {
		name := fmt.Sprintf("files=%d", fileCount)
		b.Run(name, func(b *testing.B) {
			if fileCount >= 1000000 && !benchLargeEnabled() {
				b.Skip("BLOB_BENCH_LARGE not set")
			}
			indexData := buildSyntheticIndex(fileCount, fileSize)
			idx, err := index.Load(indexData)
			if err != nil {
				b.Fatal(err)
			}
			entries := makeSyntheticEntries(fileCount, fileSize)
			path := entries[fileCount/2].Path

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				view, ok := idx.LookupView(path)
				if !ok {
					b.Fatalf("missing %s", path)
				}
				benchSinkView = view
			}

			latency := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			params := map[string]any{
				"file_count": fileCount,
			}
			reportAndEmit(b, params,
				metric("lookup_latency_ns", latency),
				metric("file_count", float64(fileCount)),
			)
		})
	}
}

func BenchmarkScaleArchiveSize(b *testing.B) {
	const fileSize = 64 << 10
	archiveSizes := []int64{10 << 20, 100 << 20, 1 << 30, 10 << 30}

	for _, archiveSize := range archiveSizes {
		b.Run("archive="+formatBytes(archiveSize), func(b *testing.B) {
			if archiveSize >= 1<<30 && !benchLargeEnabled() {
				b.Skip("BLOB_BENCH_LARGE not set")
			}

			fileCount := int(archiveSize / fileSize)
			if fileCount < 1 {
				fileCount = 1
			}
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
			indexData, dataData := createBenchArchive(b, dir, CompressionNone)
			blob, err := New(indexData, testutil.NewMockByteSource(dataData))
			if err != nil {
				b.Fatal(err)
			}
			path := paths[0]

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				content, err := blob.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				benchSinkBytes = content
			}

			latency := float64(b.Elapsed().Milliseconds()) / float64(b.N)
			params := map[string]any{
				"archive_size": len(dataData),
			}
			reportAndEmit(b, params, metric("read_latency", latency))
		})
	}
}

func BenchmarkScaleIndexMemory(b *testing.B) {
	cases := []int{1000, 10000, 100000, 1000000}
	const fileSize = 4 << 10

	for _, fileCount := range cases {
		name := fmt.Sprintf("files=%d", fileCount)
		b.Run(name, func(b *testing.B) {
			if fileCount >= 1000000 && !benchLargeEnabled() {
				b.Skip("BLOB_BENCH_LARGE not set")
			}

			indexData := buildSyntheticIndex(fileCount, fileSize)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			idx, err := index.Load(indexData)
			if err != nil {
				b.Fatal(err)
			}
			benchSinkInt = idx.Len()
			runtime.ReadMemStats(&after)
			delta := float64(after.Alloc - before.Alloc)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchSinkInt = idx.Len()
			}

			params := map[string]any{
				"file_count": fileCount,
			}
			reportAndEmit(b, params,
				metric("memory_bytes", delta),
				metric("file_count", float64(fileCount)),
			)
		})
	}
}

func BenchmarkVerifyOnRead(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 256 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	verifiedBlob, err := New(indexData, testutil.NewMockByteSource(dataData))
	if err != nil {
		b.Fatal(err)
	}
	unverifiedBlob, err := New(indexData, testutil.NewMockByteSource(dataData), WithVerifyOnClose(false))
	if err != nil {
		b.Fatal(err)
	}

	path := paths[0]
	buf := make([]byte, fileSize)
	var verifiedTotal time.Duration
	var unverifiedTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		content, err := verifiedBlob.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		benchSinkBytes = content
		verifiedTotal += time.Since(start)

		start = time.Now()
		f, err := unverifiedBlob.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		readerAt, ok := f.(io.ReaderAt)
		if !ok {
			b.Fatal("expected ReaderAt for uncompressed entry")
		}
		if _, err := readerAt.ReadAt(buf, 0); err != nil && err != io.EOF {
			b.Fatal(err)
		}
		if err := f.Close(); err != nil {
			b.Fatal(err)
		}
		unverifiedTotal += time.Since(start)
	}

	throughputVerified := throughputMBs(int64(fileSize)*int64(b.N), verifiedTotal)
	throughputUnverified := throughputMBs(int64(fileSize)*int64(b.N), unverifiedTotal)
	overhead := 0.0
	if throughputUnverified > 0 {
		overhead = 100 * ((throughputVerified / throughputUnverified) - 1)
	}
	params := map[string]any{
		"file_size": fileSize,
	}
	reportAndEmit(b, params,
		metric("throughput_verified", throughputVerified),
		metric("throughput_unverified", throughputUnverified),
		metric("overhead", overhead),
	)
}

func BenchmarkVerifyOnClose(b *testing.B) {
	const (
		fileCount = 64
		fileSize  = 256 << 10
	)

	dir := b.TempDir()
	paths := makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)
	indexData, dataData := createBenchArchive(b, dir, CompressionNone)

	inlineBlob, err := New(indexData, testutil.NewMockByteSource(dataData), WithVerifyOnClose(false))
	if err != nil {
		b.Fatal(err)
	}
	deferredBlob, err := New(indexData, testutil.NewMockByteSource(dataData), WithVerifyOnClose(true))
	if err != nil {
		b.Fatal(err)
	}

	path := paths[0]
	buf := make([]byte, 32<<10)
	var inlineTotal time.Duration
	var deferredTotal time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		f, err := inlineBlob.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = io.ReadFull(f, buf); err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			b.Fatal(err)
		}
		if _, err = io.Copy(io.Discard, f); err != nil {
			b.Fatal(err)
		}
		if err = f.Close(); err != nil {
			b.Fatal(err)
		}
		inlineTotal += time.Since(start)

		start = time.Now()
		f, err = deferredBlob.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = io.ReadFull(f, buf); err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			b.Fatal(err)
		}
		if err = f.Close(); err != nil {
			b.Fatal(err)
		}
		deferredTotal += time.Since(start)
	}

	params := map[string]any{
		"file_size": fileSize,
	}
	reportAndEmit(b, params,
		metric("latency_inline", float64(inlineTotal.Milliseconds())/float64(b.N)),
		metric("latency_deferred", float64(deferredTotal.Milliseconds())/float64(b.N)),
	)
}
