package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/meigma/blob"
	"github.com/meigma/blob/internal/testutil"
)

type config struct {
	mode            string
	files           int
	fileSize        int
	dirCount        int
	compression     string
	pattern         string
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

var (
	sinkBytes []byte
	sinkEntry blob.Entry
	sinkCount int
)

func main() {
	cfg := parseFlags()

	if cfg.pprofAddr != "" {
		go func() {
			log.Printf("pprof listening on %s", cfg.pprofAddr)
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
		defer cleanup()
	}

	paths, err := makeFiles(dir, cfg.files, cfg.fileSize, cfg.dirCount, cfg.pattern, cfg.randomSeed)
	if err != nil {
		log.Fatal(err)
	}

	index, data, err := buildArchive(dir, cfg.compression)
	if err != nil {
		log.Fatal(err)
	}

	source := testutil.NewMockByteSource(data)
	reader := blob.NewReader(index, source)

	if cfg.cpuProfile != "" {
		f, err := os.Create(cfg.cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}

	if cfg.traceFile != "" {
		f, err := os.Create(cfg.traceFile)
		if err != nil {
			log.Fatal(err)
		}
		if err := trace.Start(f); err != nil {
			log.Fatal(err)
		}
		defer func() {
			trace.Stop()
			_ = f.Close()
		}()
	}

	stats, err := runProfile(cfg, reader, paths, index, dir)
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

func runProfile(cfg config, reader *blob.Reader, paths []string, index *blob.Index, rootDir string) (profileStats, error) {
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
		readFS := fs.ReadFileFS(reader)
		if cfg.cache != "none" {
			cache, cleanup, err := newCache(cfg, rootDir)
			if err != nil {
				return profileStats{}, err
			}
			defer cleanup()
			readFS = blob.NewCachedReader(reader, cache, blob.WithPrefetchConcurrency(cfg.prefetchWorkers))
		}

		rng := rand.New(rand.NewSource(cfg.randomSeed))
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
		if cfg.cache == "none" {
			return profileStats{}, fmt.Errorf("cached-readfile-hit requires cache")
		}
		cache, cleanup, err := newCache(cfg, rootDir)
		if err != nil {
			return profileStats{}, err
		}
		defer cleanup()

		cached := blob.NewCachedReader(reader, cache, blob.WithPrefetchConcurrency(cfg.prefetchWorkers))
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
		rng := rand.New(rand.NewSource(cfg.randomSeed))
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
		rng := rand.New(rand.NewSource(cfg.randomSeed))
		for shouldContinue() {
			path := pickPath(paths, ops, rng, cfg.readRandom)
			view, ok := index.LookupView(path)
			if !ok {
				return profileStats{}, fmt.Errorf("missing entry for %q", path)
			}
			sinkEntry = entryFromViewWithPath(view, path)
			ops++
		}

	case "index-lookup-copy":
		rng := rand.New(rand.NewSource(cfg.randomSeed))
		for shouldContinue() {
			path := pickPath(paths, ops, rng, cfg.readRandom)
			view, ok := index.LookupView(path)
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
			for range index.EntriesWithPrefixView(prefix) {
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
			for view := range index.EntriesWithPrefixView(prefix) {
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
		if cfg.cache == "none" {
			return profileStats{}, fmt.Errorf("prefetchdir requires cache")
		}
		prefix := cfg.prefetchPrefix
		if prefix == "." {
			prefix = ""
		}
		prefetchBytes := prefetchSize(index, prefix)
		if cfg.prefetchCold {
			for shouldContinue() {
				cache, cleanup, err := newCache(cfg, rootDir)
				if err != nil {
					return profileStats{}, err
				}
				cached := blob.NewCachedReader(reader, cache, blob.WithPrefetchConcurrency(cfg.prefetchWorkers))
				if err := cached.PrefetchDir(prefix); err != nil {
					_ = cleanup()
					return profileStats{}, err
				}
				if err := cleanup(); err != nil {
					return profileStats{}, err
				}
				byteCount += prefetchBytes
				ops++
			}
		} else {
			cache, cleanup, err := newCache(cfg, rootDir)
			if err != nil {
				return profileStats{}, err
			}
			defer cleanup()
			cached := blob.NewCachedReader(reader, cache, blob.WithPrefetchConcurrency(cfg.prefetchWorkers))
			for shouldContinue() {
				if err := cached.PrefetchDir(prefix); err != nil {
					return profileStats{}, err
				}
				byteCount += prefetchBytes
				ops++
			}
		}

	case "writer":
		w := blob.NewWriter(blob.WriteOptions{Compression: parseCompression(cfg.compression)})
		var indexBuf, dataBuf bytes.Buffer
		for shouldContinue() {
			indexBuf.Reset()
			dataBuf.Reset()
			if err := w.Create(context.Background(), rootDir, &indexBuf, &dataBuf); err != nil {
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
	flag.StringVar(&cfg.mode, "mode", "readfile", "mode: readfile, cached-readfile-hit, prefetchdir, writer, index-lookup, index-lookup-copy, entries-with-prefix, entries-with-prefix-copy")
	flag.IntVar(&cfg.files, "files", 512, "number of files")
	flag.IntVar(&cfg.fileSize, "file-size", 16<<10, "file size in bytes")
	flag.IntVar(&cfg.dirCount, "dir-count", 16, "number of directories")
	flag.StringVar(&cfg.compression, "compression", "zstd", "compression: none or zstd")
	flag.StringVar(&cfg.pattern, "pattern", "compressible", "pattern: compressible or random")
	flag.DurationVar(&cfg.duration, "duration", 10*time.Second, "duration to run (ignored if iterations > 0)")
	flag.IntVar(&cfg.iterations, "iterations", 0, "number of iterations to run")
	flag.StringVar(&cfg.pprofAddr, "pprof-addr", "", "pprof listen address (e.g. :6060)")
	flag.StringVar(&cfg.cpuProfile, "cpuprofile", "", "write CPU profile to file")
	flag.StringVar(&cfg.memProfile, "memprofile", "", "write heap profile to file")
	flag.StringVar(&cfg.traceFile, "trace", "", "write trace to file")
	flag.StringVar(&cfg.cache, "cache", "memory", "cache: memory, disk, none")
	flag.StringVar(&cfg.cacheDir, "cache-dir", "", "cache directory (disk cache only)")
	flag.StringVar(&cfg.prefetchPrefix, "prefix", "dir00", "prefix for prefetchdir and entries-with-prefix modes")
	flag.BoolVar(&cfg.prefetchCold, "prefetch-cold", true, "recreate cache each prefetchdir iteration")
	flag.IntVar(&cfg.prefetchWorkers, "prefetch-workers", 0, "prefetch workers: <0 serial, 0 auto, >0 fixed")
	flag.BoolVar(&cfg.readRandom, "read-random", true, "randomize readfile path selection")
	flag.StringVar(&cfg.tempDir, "temp-dir", "", "directory to use for dataset")
	flag.BoolVar(&cfg.keepTemp, "keep-temp", false, "keep temp dir after run")
	flag.Int64Var(&cfg.randomSeed, "seed", 1, "random seed")
	flag.Parse()
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

func entryFromViewWithPath(view blob.EntryView, path string) blob.Entry {
	return blob.Entry{
		Path:         path,
		DataOffset:   view.DataOffset(),
		DataSize:     view.DataSize(),
		OriginalSize: view.OriginalSize(),
		Hash:         view.HashBytes(),
		Mode:         view.Mode(),
		UID:          view.UID(),
		GID:          view.GID(),
		ModTime:      view.ModTime(),
		Compression:  view.Compression(),
	}
}

func setupTempDir(cfg config) (string, func() error, error) {
	if cfg.tempDir != "" {
		return cfg.tempDir, nil, os.MkdirAll(cfg.tempDir, 0o755)
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
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < fileCount; i++ {
		relPath := fmt.Sprintf("dir%02d/file%05d.dat", i%dirCount, i)
		fullPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
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

		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			return nil, err
		}
		paths = append(paths, relPath)
	}
	return paths, nil
}

func buildArchive(root, compression string) (*blob.Index, []byte, error) {
	var indexBuf, dataBuf bytes.Buffer
	w := blob.NewWriter(blob.WriteOptions{Compression: parseCompression(compression)})
	if err := w.Create(context.Background(), root, &indexBuf, &dataBuf); err != nil {
		return nil, nil, err
	}
	idx, err := blob.LoadIndex(indexBuf.Bytes())
	if err != nil {
		return nil, nil, err
	}
	return idx, dataBuf.Bytes(), nil
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

func prefetchSize(index *blob.Index, prefix string) int64 {
	var total uint64
	for view := range index.EntriesWithPrefixView(prefix) {
		size := view.OriginalSize()
		next := total + size
		if next < total {
			return int64(^uint64(0) >> 1)
		}
		total = next
	}
	return int64(total)
}

func newCache(cfg config, rootDir string) (blob.Cache, func() error, error) {
	switch cfg.cache {
	case "none":
		return nil, nil, fmt.Errorf("cache=none should not create a cache")
	case "memory":
		return testutil.NewMockCache(), func() error { return nil }, nil
	case "disk":
		cacheDir := cfg.cacheDir
		autoDir := false
		if cacheDir == "" {
			base := filepath.Join(rootDir, "cache")
			if err := os.MkdirAll(base, 0o755); err != nil {
				return nil, nil, err
			}
			dir, err := os.MkdirTemp(base, "run-*")
			if err != nil {
				return nil, nil, err
			}
			cacheDir = dir
			autoDir = true
		} else if err := os.MkdirAll(cacheDir, 0o755); err != nil {
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

func (c *streamingDiskCache) Writer(hash []byte) (blob.CacheWriter, error) {
	writer, err := c.DiskCache.Writer(hash)
	if err != nil {
		return nil, err
	}
	adapted, ok := writer.(blob.CacheWriter)
	if !ok {
		return nil, fmt.Errorf("unexpected cache writer type %T", writer)
	}
	return adapted, nil
}
