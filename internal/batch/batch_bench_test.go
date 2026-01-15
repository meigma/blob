package batch

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	blobhttp "github.com/meigma/blob/http"
	"github.com/meigma/blob/internal/file"
	"github.com/meigma/blob/internal/testutil"
)

var benchBatchSinkBytes int64

type benchBatchCase struct {
	name        string
	fileCount   int
	fileSize    int
	groupSize   int
	gapSize     int
	compression Compression
}

type benchBatchSource struct {
	name string
	new  func(b *testing.B, data []byte) (file.ByteSource, func(), error)
}

func BenchmarkProcessorPipelined(b *testing.B) {
	cases := []benchBatchCase{
		{
			name:        "files=512/size=16k/groups=1/gap=0/none",
			fileCount:   512,
			fileSize:    16 << 10,
			groupSize:   512,
			gapSize:     0,
			compression: CompressionNone,
		},
		{
			name:        "files=512/size=16k/groups=32/gap=4k/none",
			fileCount:   512,
			fileSize:    16 << 10,
			groupSize:   16,
			gapSize:     4 << 10,
			compression: CompressionNone,
		},
		{
			name:        "files=512/size=16k/groups=32/gap=4k/zstd",
			fileCount:   512,
			fileSize:    16 << 10,
			groupSize:   16,
			gapSize:     4 << 10,
			compression: CompressionZstd,
		},
	}

	sources := benchBatchSources()
	modes := []struct {
		name string
		opts []ProcessorOption
	}{
		{name: "mode=sequential"},
		{name: "mode=pipelined", opts: []ProcessorOption{WithReadConcurrency(4)}},
	}

	for _, bc := range cases {
		entries, data, pool, totalBytes := buildBenchBatchData(b, bc)

		for _, source := range sources {
			for _, mode := range modes {
				name := fmt.Sprintf("%s/%s/%s", bc.name, source.name, mode.name)
				b.Run(name, func(b *testing.B) {
					src, cleanup, err := source.new(b, data)
					if err != nil {
						b.Fatal(err)
					}
					if cleanup != nil {
						defer cleanup()
					}

					procOpts := append([]ProcessorOption{WithWorkers(-1)}, mode.opts...)
					proc := NewProcessor(src, pool, 0, procOpts...)
					sink := &benchDiscardSink{}

					if totalBytes > 0 {
						b.SetBytes(totalBytes)
					}
					b.ReportAllocs()
					b.ResetTimer()

					for b.Loop() {
						if err := proc.Process(entries, sink); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func buildBenchBatchData(b *testing.B, bc benchBatchCase) (entries []*Entry, data []byte, pool *file.DecompressPool, totalBytes int64) {
	b.Helper()

	groupSize := bc.groupSize
	if groupSize <= 0 || groupSize > bc.fileCount {
		groupSize = bc.fileCount
	}

	entries = make([]*Entry, 0, bc.fileCount)
	data = make([]byte, 0, bc.fileCount*bc.fileSize)
	var offset uint64

	var enc *zstd.Encoder
	if bc.compression == CompressionZstd {
		var err error
		enc, err = zstd.NewWriter(nil)
		if err != nil {
			b.Fatal(err)
		}
		defer enc.Close()
	}

	for i := range bc.fileCount {
		content := bytes.Repeat([]byte{byte('a' + (i % 26))}, bc.fileSize)
		sum := sha256.Sum256(content)
		entryData := content
		if enc != nil {
			entryData = enc.EncodeAll(content, nil)
		}

		entry := &Entry{
			Path:         fmt.Sprintf("file%05d", i),
			DataOffset:   offset,
			DataSize:     uint64(len(entryData)),
			OriginalSize: uint64(len(content)),
			Hash:         sum[:],
			Compression:  bc.compression,
		}
		entries = append(entries, entry)

		data = append(data, entryData...)
		offset += uint64(len(entryData))
		totalBytes += int64(len(entryData))

		if (i+1)%groupSize == 0 && i+1 < bc.fileCount && bc.gapSize > 0 {
			data = append(data, make([]byte, bc.gapSize)...)
			offset += uint64(bc.gapSize)
		}
	}

	if bc.compression == CompressionZstd {
		pool = file.NewDecompressPool(file.DefaultMaxDecoderMemory)
	}

	return entries, data, pool, totalBytes
}

type benchDiscardSink struct{}

func (s *benchDiscardSink) ShouldProcess(*Entry) bool {
	return true
}

func (s *benchDiscardSink) Writer(*Entry) (Committer, error) {
	return &benchDiscardCommitter{}, nil
}

func (s *benchDiscardSink) PutBuffered(_ *Entry, content []byte) error {
	atomic.AddInt64(&benchBatchSinkBytes, int64(len(content)))
	return nil
}

type benchDiscardCommitter struct{}

func (c *benchDiscardCommitter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *benchDiscardCommitter) Commit() error {
	return nil
}

func (c *benchDiscardCommitter) Discard() error {
	return nil
}

func benchBatchSources() []benchBatchSource {
	sources := []benchBatchSource{
		{
			name: "source=memory",
			new: func(_ *testing.B, data []byte) (file.ByteSource, func(), error) {
				return testutil.NewMockByteSource(data), nil, nil
			},
		},
	}
	if os.Getenv("BLOB_BENCH_HTTP") == "" {
		return sources
	}
	sources = append(sources, benchBatchSource{
		name: "source=http",
		new: func(b *testing.B, data []byte) (file.ByteSource, func(), error) { //nolint:thelper // not a test helper, factory function
			cfg, err := benchBatchHTTPConfigFromEnv()
			if err != nil {
				return nil, nil, err
			}
			client := benchBatchHTTPClient(cfg)
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
		},
	})
	return sources
}

type benchBatchHTTPConfig struct {
	latency        time.Duration
	bytesPerSecond int64
}

func benchBatchHTTPConfigFromEnv() (benchBatchHTTPConfig, error) {
	var cfg benchBatchHTTPConfig
	if value := strings.TrimSpace(os.Getenv("BLOB_HTTP_LATENCY")); value != "" {
		latency, err := time.ParseDuration(value)
		if err != nil {
			return cfg, fmt.Errorf("BLOB_HTTP_LATENCY: %w", err)
		}
		cfg.latency = latency
	}
	if value := strings.TrimSpace(os.Getenv("BLOB_HTTP_BPS")); value != "" {
		bps, err := parseBenchBatchBytesPerSecond(value)
		if err != nil {
			return cfg, fmt.Errorf("BLOB_HTTP_BPS: %w", err)
		}
		cfg.bytesPerSecond = bps
	}
	return cfg, nil
}

func parseBenchBatchBytesPerSecond(value string) (int64, error) {
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

type benchBatchHTTPRoundTripper struct {
	base           nethttp.RoundTripper
	latency        time.Duration
	bytesPerSecond int64
}

func (rt *benchBatchHTTPRoundTripper) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	if rt.latency > 0 {
		time.Sleep(rt.latency)
	}
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if rt.bytesPerSecond > 0 && resp.Body != nil {
		resp.Body = &benchBatchThrottleReadCloser{
			rc:             resp.Body,
			bytesPerSecond: rt.bytesPerSecond,
			start:          time.Now(),
		}
	}
	return resp, nil
}

func benchBatchHTTPClient(cfg benchBatchHTTPConfig) *nethttp.Client {
	transport := nethttp.DefaultTransport
	if base, ok := transport.(*nethttp.Transport); ok {
		transport = base.Clone()
	}
	if cfg.latency > 0 || cfg.bytesPerSecond > 0 {
		transport = &benchBatchHTTPRoundTripper{
			base:           transport,
			latency:        cfg.latency,
			bytesPerSecond: cfg.bytesPerSecond,
		}
	}
	return &nethttp.Client{Transport: transport}
}

type benchBatchThrottleReadCloser struct {
	rc             io.ReadCloser
	bytesPerSecond int64
	start          time.Time
	readBytes      int64
}

func (tr *benchBatchThrottleReadCloser) Read(p []byte) (int, error) {
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

func (tr *benchBatchThrottleReadCloser) Close() error {
	return tr.rc.Close()
}
