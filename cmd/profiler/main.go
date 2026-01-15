package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/rand" //nolint:gosec // intentional use for reproducible benchmarks
	"net/http"
	_ "net/http/pprof" //nolint:gosec // intentional profiling endpoint
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/felixge/fgprof"
	"github.com/meigma/blob"
	"github.com/meigma/blob/cache"
	"github.com/meigma/blob/internal/testutil"
)

const cacheNone = "none"

type config struct {
	mode            string
	files           int
	fileSize        int
	dirCount        int
	compression     string
	pattern         string
	dataURL         string
	dataHTTPLatency time.Duration
	dataHTTPBPS     int64
	fgProfile       string
	duration        time.Duration
	iterations      int
	pprofAddr       string
	cpuProfile      string
	memProfile      string
	traceFile       string
	cache           string
	cacheDir        string
	prefetchPrefix  string
	prefetchCold    bool
	prefetchWorkers int
	readRandom      bool
	tempDir         string
	keepTemp        bool
	randomSeed      int64
}

//nolint:unused // sink variables prevent compiler optimizations in profiling
var (
	sinkBytes []byte
	sinkEntry blob.Entry
	sinkCount int
)

//nolint:gocognit,gocyclo // main function complexity is acceptable for CLI tool
func main() {
	cfg := parseFlags()

	if cfg.pprofAddr != "" {
		go func() {
			log.Printf("pprof listening on %s", cfg.pprofAddr)
			//nolint:gosec // intentional pprof server without timeouts for profiling
			if err := http.ListenAndServe(cfg.pprofAddr, nil); err != nil {
				log.Printf("pprof server error: %v", err)
			}
		}()
	}

	dir, cleanup, err := setupTempDir(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if cleanup != nil {
		defer cleanup() //nolint:errcheck // cleanup errors are non-fatal in profiler
	}

	paths, err := makeFiles(dir, cfg.files, cfg.fileSize, cfg.dirCount, cfg.pattern, cfg.randomSeed)
	if err != nil {
		log.Fatal(err) //nolint:gocritic // exitAfterDefer is intentional - cleanup is best-effort
	}

	b, cleanupArchive, err := buildArchive(dir, cfg)
	if err != nil {
		log.Fatal(err)
	}
	if cleanupArchive != nil {
		defer cleanupArchive()
	}

	var stopFG func() error
	if cfg.fgProfile != "" {
		fgFile, fgErr := os.Create(cfg.fgProfile)
		if fgErr != nil {
			log.Fatal(fgErr)
		}
		stopFG = fgprof.Start(fgFile, fgprof.FormatPprof)
		defer func() {
			if err := stopFG(); err != nil {
				log.Printf("fgprof stop error: %v", err)
			}
			_ = fgFile.Close()
		}()
	}

	if cfg.cpuProfile != "" {
		cpuFile, cpuErr := os.Create(cfg.cpuProfile)
		if cpuErr != nil {
			log.Fatal(cpuErr)
		}
		if cpuErr = pprof.StartCPUProfile(cpuFile); cpuErr != nil {
			log.Fatal(cpuErr)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = cpuFile.Close()
		}()
	}

	if cfg.traceFile != "" {
		traceFile, traceErr := os.Create(cfg.traceFile)
		if traceErr != nil {
			log.Fatal(traceErr)
		}
		if traceErr = trace.Start(traceFile); traceErr != nil {
			log.Fatal(traceErr)
		}
		defer func() {
			trace.Stop()
			_ = traceFile.Close()
		}()
	}

	stats, err := runProfile(cfg, b, paths, dir)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.memProfile != "" {
		runtime.GC()
		f, err := os.Create(cfg.memProfile)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal(err)
		}
		_ = f.Close()
	}

	fmt.Printf("mode=%s ops=%d bytes=%d elapsed=%s throughput=%.2f MB/s\n",
		cfg.mode,
		stats.ops,
		stats.bytes,
		stats.elapsed,
		float64(stats.bytes)/(1024*1024)/stats.elapsed.Seconds(),
	)
}

type profileStats struct {
	ops     int
	bytes   int64
	elapsed time.Duration
}

//nolint:gocognit,gocyclo,gocritic // complexity is inherent to multi-mode profiler dispatch; hugeParam acceptable for profiler
func runProfile(cfg config, b *blob.Blob, paths []string, rootDir string) (profileStats, error) {
	start := time.Now()
	ops := 0
	var byteCount int64

	shouldContinue := func() bool {
		if cfg.iterations > 0 {
			return ops < cfg.iterations
		}
		return time.Since(start) < cfg.duration
	}

	switch cfg.mode {
	case "readfile":
		readFS := fs.ReadFileFS(b)
		if cfg.cache != cacheNone {
			c, cleanup, err := newCache(cfg, rootDir)
			if err != nil {
				return profileStats{}, err
			}
			defer cleanup() //nolint:errcheck // cleanup errors are non-fatal in profiler
			readFS = cache.New(b, c, cache.WithPrefetchConcurrency(cfg.prefetchWorkers))
		}

		rng := rand.New(rand.NewSource(cfg.randomSeed)) //nolint:gosec // intentional for reproducible benchmarks
		for shouldContinue() {
			path := pickPath(paths, ops, rng, cfg.readRandom)
			content, err := readFS.ReadFile(path)
			if err != nil {
				return profileStats{}, err
			}
			sinkBytes = content
			byteCount += int64(len(content))
			ops++
		}

	case "cached-readfile-hit":
		if cfg.cache == cacheNone {
			return profileStats{}, errors.New("cached-readfile-hit requires cache")
		}
		c, cleanup, err := newCache(cfg, rootDir)
		if err != nil {
			return profileStats{}, err
		}
		defer cleanup() //nolint:errcheck // cleanup errors are non-fatal in profiler

		cached := cache.New(b, c, cache.WithPrefetchConcurrency(cfg.prefetchWorkers))
		for _, path := range paths {
			content, err := cached.ReadFile(path)
			if err != nil {
				return profileStats{}, err
			}
			sinkBytes = content
		}

		start = time.Now()
		ops = 0
		byteCount = 0
		rng := rand.New(rand.NewSource(cfg.randomSeed)) //nolint:gosec // intentional for reproducible benchmarks
		for shouldContinue() {
			path := pickPath(paths, ops, rng, cfg.readRandom)
			content, err := cached.ReadFile(path)
			if err != nil {
				return profileStats{}, err
			}
			sinkBytes = content
			byteCount += int64(len(content))
			ops++
		}

	case "index-lookup":
		rng := rand.New(rand.NewSource(cfg.randomSeed)) //nolint:gosec // intentional for reproducible benchmarks
		for shouldContinue() {
			path := pickPath(paths, ops, rng, cfg.readRandom)
			view, ok := b.Entry(path)
			if !ok {
				return profileStats{}, fmt.Errorf("missing entry for %q", path)
			}
			sinkEntry = blob.EntryFromViewWithPath(view, path)
			ops++
		}

	case "index-lookup-copy":
		rng := rand.New(rand.NewSource(cfg.randomSeed)) //nolint:gosec // intentional for reproducible benchmarks
		for shouldContinue() {
			path := pickPath(paths, ops, rng, cfg.readRandom)
			view, ok := b.Entry(path)
			if !ok {
				return profileStats{}, fmt.Errorf("missing entry for %q", path)
			}
			sinkEntry = view.Entry()
			ops++
		}

	case "entries-with-prefix":
		prefix := scanPrefix(cfg.prefetchPrefix)
		for shouldContinue() {
			count := 0
			for range b.EntriesWithPrefix(prefix) {
				count++
			}
			if count == 0 {
				return profileStats{}, fmt.Errorf("expected at least one entry for prefix %q", prefix)
			}
			sinkCount = count
			ops++
		}

	case "entries-with-prefix-copy":
		prefix := scanPrefix(cfg.prefetchPrefix)
		for shouldContinue() {
			count := 0
			for view := range b.EntriesWithPrefix(prefix) {
				sinkEntry = view.Entry()
				count++
			}
			if count == 0 {
				return profileStats{}, fmt.Errorf("expected at least one entry for prefix %q", prefix)
			}
			sinkCount = count
			ops++
		}

	case "prefetchdir":
		if cfg.cache == cacheNone {
			return profileStats{}, errors.New("prefetchdir requires cache")
		}
		prefix := cfg.prefetchPrefix
		if prefix == "." {
			prefix = ""
		}
		prefetchBytes := prefetchSize(b, prefix)
		if cfg.prefetchCold {
			for shouldContinue() {
				c, cleanup, err := newCache(cfg, rootDir)
				if err != nil {
					return profileStats{}, err
				}
				cached := cache.New(b, c, cache.WithPrefetchConcurrency(cfg.prefetchWorkers))
				if err := cached.PrefetchDir(prefix); err != nil {
					_ = cleanup() //nolint:errcheck // cleanup errors are non-fatal in profiler
					return profileStats{}, err
				}
				if err := cleanup(); err != nil {
					return profileStats{}, err
				}
				byteCount += prefetchBytes
				ops++
			}
		} else {
			c, cleanup, err := newCache(cfg, rootDir)
			if err != nil {
				return profileStats{}, err
			}
			defer cleanup() //nolint:errcheck // cleanup errors are non-fatal in profiler
			cached := cache.New(b, c, cache.WithPrefetchConcurrency(cfg.prefetchWorkers))
			for shouldContinue() {
				if err := cached.PrefetchDir(prefix); err != nil {
					return profileStats{}, err
				}
				byteCount += prefetchBytes
				ops++
			}
		}

	case "copydir":
		prefix := cfg.prefetchPrefix
		if prefix == "." {
			prefix = ""
		}
		copyBytes := prefetchSize(b, scanPrefix(prefix))
		opts := []blob.CopyOption{}
		if cfg.prefetchWorkers != 0 {
			opts = append(opts, blob.CopyWithWorkers(cfg.prefetchWorkers))
		}

		if cfg.prefetchCold {
			for shouldContinue() {
				destDir := filepath.Join(rootDir, "copy", fmt.Sprintf("iter-%d", ops))
				if err := os.MkdirAll(destDir, 0o750); err != nil {
					return profileStats{}, err
				}
				if err := b.CopyDir(destDir, prefix, opts...); err != nil {
					return profileStats{}, err
				}
				if err := os.RemoveAll(destDir); err != nil {
					return profileStats{}, err
				}
				byteCount += copyBytes
				ops++
			}
		} else {
			destDir := filepath.Join(rootDir, "copy")
			if err := os.MkdirAll(destDir, 0o750); err != nil {
				return profileStats{}, err
			}
			opts = append(opts, blob.CopyWithOverwrite(true))
			for shouldContinue() {
				if err := b.CopyDir(destDir, prefix, opts...); err != nil {
					return profileStats{}, err
				}
				byteCount += copyBytes
				ops++
			}
		}

	case "writer":
		var indexBuf, dataBuf bytes.Buffer
		var opts []blob.CreateOption
		if parseCompression(cfg.compression) != blob.CompressionNone {
			opts = append(opts, blob.CreateWithCompression(parseCompression(cfg.compression)))
		}
		for shouldContinue() {
			indexBuf.Reset()
			dataBuf.Reset()
			if err := blob.Create(context.Background(), rootDir, &indexBuf, &dataBuf, opts...); err != nil {
				return profileStats{}, err
			}
			byteCount += int64(dataBuf.Len())
			ops++
		}

	default:
		return profileStats{}, fmt.Errorf("unknown mode: %s", cfg.mode)
	}

	return profileStats{
		ops:     ops,
		bytes:   byteCount,
		elapsed: time.Since(start),
	}, nil
}

func parseFlags() config {
	var cfg config
	var dataHTTPBPS string
	flag.StringVar(&cfg.mode, "mode", "readfile", "mode: readfile, cached-readfile-hit, prefetchdir, copydir, writer, index-lookup, index-lookup-copy, entries-with-prefix, entries-with-prefix-copy")
	flag.IntVar(&cfg.files, "files", 512, "number of files")
	flag.IntVar(&cfg.fileSize, "file-size", 16<<10, "file size in bytes")
	flag.IntVar(&cfg.dirCount, "dir-count", 16, "number of directories")
	flag.StringVar(&cfg.compression, "compression", "zstd", "compression: none or zstd")
	flag.StringVar(&cfg.pattern, "pattern", "compressible", "pattern: compressible or random")
	flag.StringVar(&cfg.dataURL, "data-url", "", "HTTP data source URL (use \"local\" to serve generated data)")
	flag.DurationVar(&cfg.dataHTTPLatency, "data-http-latency", 0, "per-request latency for HTTP data source")
	flag.StringVar(&dataHTTPBPS, "data-http-bps", "", "bytes/sec throttle for HTTP data source (e.g. 10MBps)")
	flag.StringVar(&cfg.fgProfile, "fgprofile", "", "write fgprof (wall clock) profile to file")
	flag.DurationVar(&cfg.duration, "duration", 10*time.Second, "duration to run (ignored if iterations > 0)")
	flag.IntVar(&cfg.iterations, "iterations", 0, "number of iterations to run")
	flag.StringVar(&cfg.pprofAddr, "pprof-addr", "", "pprof listen address (e.g. :6060)")
	flag.StringVar(&cfg.cpuProfile, "cpuprofile", "", "write CPU profile to file")
	flag.StringVar(&cfg.memProfile, "memprofile", "", "write heap profile to file")
	flag.StringVar(&cfg.traceFile, "trace", "", "write trace to file")
	flag.StringVar(&cfg.cache, "cache", "memory", "cache: memory, disk, none")
	flag.StringVar(&cfg.cacheDir, "cache-dir", "", "cache directory (disk cache only)")
	flag.StringVar(&cfg.prefetchPrefix, "prefix", "dir00", "prefix for prefetchdir, copydir, and entries-with-prefix modes")
	flag.BoolVar(&cfg.prefetchCold, "prefetch-cold", true, "recreate cache or copy destination each iteration")
	flag.IntVar(&cfg.prefetchWorkers, "prefetch-workers", 0, "prefetch/copy workers: <0 serial, 0 auto, >0 fixed")
	flag.BoolVar(&cfg.readRandom, "read-random", true, "randomize readfile path selection")
	flag.StringVar(&cfg.tempDir, "temp-dir", "", "directory to use for dataset")
	flag.BoolVar(&cfg.keepTemp, "keep-temp", false, "keep temp dir after run")
	flag.Int64Var(&cfg.randomSeed, "seed", 1, "random seed")
	flag.Parse()
	if dataHTTPBPS != "" {
		bps, err := parseBytesPerSecond(dataHTTPBPS)
		if err != nil {
			log.Fatalf("data-http-bps: %v", err)
		}
		cfg.dataHTTPBPS = bps
	}
	return cfg
}

func pickPath(paths []string, idx int, rng *rand.Rand, random bool) string {
	if random {
		return paths[rng.Intn(len(paths))]
	}
	return paths[idx%len(paths)]
}

func scanPrefix(prefix string) string {
	if prefix == "." {
		return ""
	}
	if prefix == "" {
		return ""
	}
	if prefix[len(prefix)-1] != '/' {
		return prefix + "/"
	}
	return prefix
}

//nolint:gocritic // hugeParam acceptable for config struct in CLI tool
func setupTempDir(cfg config) (string, func() error, error) {
	if cfg.tempDir != "" {
		return cfg.tempDir, nil, os.MkdirAll(cfg.tempDir, 0o755) //nolint:gosec // 0o755 is intentional for profiler temp dirs
	}
	dir, err := os.MkdirTemp("", "blob-profiler-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() error {
		if cfg.keepTemp {
			return nil
		}
		return os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

func makeFiles(dir string, fileCount, fileSize, dirCount int, pattern string, seed int64) ([]string, error) {
	if dirCount <= 0 {
		dirCount = 1
	}
	paths := make([]string, 0, fileCount)
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // intentional use for reproducible benchmarks
	for i := range fileCount {
		relPath := fmt.Sprintf("dir%02d/file%05d.dat", i%dirCount, i)
		fullPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil { //nolint:gosec // 0o755 is intentional for profiler
			return nil, err
		}

		content := make([]byte, fileSize)
		switch pattern {
		case "random":
			if _, err := rng.Read(content); err != nil {
				return nil, err
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

		if err := os.WriteFile(fullPath, content, 0o644); err != nil { //nolint:gosec // 0o644 is intentional for profiler test files
			return nil, err
		}
		paths = append(paths, relPath)
	}
	return paths, nil
}

func buildArchive(root string, cfg config) (*blob.Blob, func(), error) {
	var indexBuf, dataBuf bytes.Buffer
	var opts []blob.CreateOption
	if parseCompression(cfg.compression) != blob.CompressionNone {
		opts = append(opts, blob.CreateWithCompression(parseCompression(cfg.compression)))
	}
	if err := blob.Create(context.Background(), root, &indexBuf, &dataBuf, opts...); err != nil {
		return nil, nil, err
	}
	if cfg.dataURL == "" {
		b, err := blob.New(indexBuf.Bytes(), testutil.NewMockByteSource(dataBuf.Bytes()))
		if err != nil {
			return nil, nil, err
		}
		return b, nil, nil
	}

	source, cleanup, err := newHTTPSource(cfg, dataBuf.Bytes())
	if err != nil {
		return nil, nil, err
	}
	b, err := blob.New(indexBuf.Bytes(), source)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, nil, err
	}
	return b, cleanup, nil
}

func parseCompression(name string) blob.Compression {
	switch name {
	case "none":
		return blob.CompressionNone
	case "zstd":
		return blob.CompressionZstd
	default:
		log.Fatalf("unknown compression: %s", name)
		return blob.CompressionNone
	}
}

func prefetchSize(b *blob.Blob, prefix string) int64 {
	var total uint64
	for view := range b.EntriesWithPrefix(prefix) {
		size := view.OriginalSize()
		next := total + size
		if next < total {
			return int64(^uint64(0) >> 1) //nolint:gosec // overflow check is explicit above
		}
		total = next
	}
	return int64(total) //nolint:gosec // overflow is checked and bounded above
}

//nolint:gocritic // hugeParam acceptable for config struct in CLI tool
func newCache(cfg config, rootDir string) (cache.Cache, func() error, error) {
	switch cfg.cache {
	case cacheNone:
		return nil, nil, errors.New("cache=none should not create a cache")
	case "memory":
		return testutil.NewMockCache(), func() error { return nil }, nil
	case "disk":
		cacheDir := cfg.cacheDir
		autoDir := false
		if cacheDir == "" {
			base := filepath.Join(rootDir, "cache")
			if err := os.MkdirAll(base, 0o755); err != nil { //nolint:gosec // 0o755 is intentional for profiler
				return nil, nil, err
			}
			dir, err := os.MkdirTemp(base, "run-*")
			if err != nil {
				return nil, nil, err
			}
			cacheDir = dir
			autoDir = true
		} else if err := os.MkdirAll(cacheDir, 0o755); err != nil { //nolint:gosec // 0o755 is intentional for profiler
			return nil, nil, err
		}

		c := &streamingDiskCache{DiskCache: testutil.NewDiskCache(cacheDir)}
		cleanup := func() error {
			if autoDir {
				return os.RemoveAll(cacheDir)
			}
			return nil
		}
		return c, cleanup, nil
	default:
		return nil, nil, fmt.Errorf("unknown cache: %s", cfg.cache)
	}
}

type streamingDiskCache struct {
	*testutil.DiskCache
}

func (c *streamingDiskCache) Writer(hash []byte) (cache.Writer, error) {
	writer, err := c.DiskCache.Writer(hash)
	if err != nil {
		return nil, err
	}
	adapted, ok := writer.(cache.Writer)
	if !ok {
		return nil, fmt.Errorf("unexpected cache writer type %T", writer)
	}
	return adapted, nil
}
