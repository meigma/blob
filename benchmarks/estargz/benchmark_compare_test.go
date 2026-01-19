package estargzbench

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/stargz-snapshotter/estargz"
	"github.com/containerd/stargz-snapshotter/estargz/zstdchunked"
	"github.com/klauspost/compress/zstd"
	"github.com/meigma/blob"
	blobhttp "github.com/meigma/blob/http"
)

var (
	sinkBytes  []byte
	sinkBlob   *blob.Blob
	sinkReader *estargz.Reader
)

type benchPattern string

const (
	benchPatternCompressible benchPattern = "compressible"
	benchPatternRandom       benchPattern = "random"

	benchDirCount = 16
)

type formatKind int

const (
	formatBlob formatKind = iota
	formatEStargz
)

type benchFormat struct {
	name               string
	kind               formatKind
	blobCompression    blob.Compression
	estargzOptions     []estargz.Option
	estargzOpenOptions []estargz.OpenOption
}

func benchFormats() []benchFormat {
	formats := []benchFormat{
		{
			name:            "format=blob/none",
			kind:            formatBlob,
			blobCompression: blob.CompressionNone,
		},
		{
			name:            "format=blob/zstd",
			kind:            formatBlob,
			blobCompression: blob.CompressionZstd,
		},
		{
			name: "format=estargz/gzip",
			kind: formatEStargz,
		},
	}
	if os.Getenv("ESTARGZ_BENCH_ZSTDCHUNKED") != "" {
		formats = append(formats, benchFormat{
			name: "format=estargz/zstdchunked",
			kind: formatEStargz,
			estargzOptions: []estargz.Option{
				estargz.WithCompression(newZstdChunkedCompression()),
			},
			estargzOpenOptions: []estargz.OpenOption{
				estargz.WithDecompressors(new(zstdchunked.Decompressor)),
			},
		})
	}
	return formats
}

type zstdChunkedCompression struct {
	*zstdchunked.Compressor
	*zstdchunked.Decompressor
}

func newZstdChunkedCompression() estargz.Compression {
	return &zstdChunkedCompression{
		Compressor: &zstdchunked.Compressor{
			CompressionLevel: zstd.SpeedDefault,
		},
		Decompressor: &zstdchunked.Decompressor{},
	}
}

type blobArchive struct {
	indexData []byte
	dataData  []byte
}

func BenchmarkCompareBuild(b *testing.B) {
	cases := []struct {
		name      string
		fileCount int
		fileSize  int
		pattern   benchPattern
	}{
		{name: "files=128/size=16k/compressible", fileCount: 128, fileSize: 16 << 10, pattern: benchPatternCompressible},
		{name: "files=128/size=16k/random", fileCount: 128, fileSize: 16 << 10, pattern: benchPatternRandom},
	}

	formats := benchFormats()

	for _, bc := range cases {
		dir := b.TempDir()
		makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, bc.pattern)
		tarData := buildTarFromDir(b, dir)
		totalBytes := int64(bc.fileCount * bc.fileSize)

		for _, format := range formats {
			format := format
			b.Run(fmt.Sprintf("%s/%s", bc.name, format.name), func(b *testing.B) {
				if totalBytes > 0 {
					b.SetBytes(totalBytes)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					switch format.kind {
					case formatBlob:
						var indexBuf, dataBuf bytes.Buffer
						var opts []blob.CreateOption
						if format.blobCompression != blob.CompressionNone {
							opts = append(opts, blob.CreateWithCompression(format.blobCompression))
						}
						if err := blob.Create(context.Background(), dir, &indexBuf, &dataBuf, opts...); err != nil {
							b.Fatal(err)
						}
						sinkBytes = dataBuf.Bytes()
					case formatEStargz:
						sr := io.NewSectionReader(bytes.NewReader(tarData), 0, int64(len(tarData)))
						rc, err := estargz.Build(sr, format.estargzOptions...)
						if err != nil {
							b.Fatal(err)
						}
						if _, err := io.Copy(io.Discard, rc); err != nil {
							rc.Close()
							b.Fatal(err)
						}
						if err := rc.Close(); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func BenchmarkCompareOpenHTTP(b *testing.B) {
	if !benchHTTPEnabled() {
		b.Skip("BLOB_BENCH_HTTP not set")
	}

	const (
		fileCount = 128
		fileSize  = 16 << 10
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)

	cfg, err := benchHTTPConfigFromEnv()
	if err != nil {
		b.Fatal(err)
	}
	client := benchHTTPClient(cfg)

	formats := benchFormats()
	blobData := make(map[blob.Compression]blobArchive)
	estargzData := make(map[string][]byte)

	for _, format := range formats {
		switch format.kind {
		case formatBlob:
			if _, ok := blobData[format.blobCompression]; !ok {
				indexData, dataData := buildBlobArchive(b, dir, format.blobCompression)
				blobData[format.blobCompression] = blobArchive{indexData: indexData, dataData: dataData}
			}
		case formatEStargz:
			if _, ok := estargzData[format.name]; !ok {
				estargzData[format.name] = buildEStargzArchive(b, dir, format.estargzOptions...)
			}
		}
	}

	for _, format := range formats {
		format := format
		b.Run(format.name, func(b *testing.B) {
			switch format.kind {
			case formatBlob:
				archive := blobData[format.blobCompression]
				server := newBenchHTTPServer(archive.dataData, archive.indexData)
				defer server.Close()

				dataURL := server.URL + "/data"
				indexURL := server.URL + "/index"
				source, err := blobhttp.NewSource(dataURL, blobhttp.WithClient(client))
				if err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					indexData, err := fetchHTTPBytes(client, indexURL)
					if err != nil {
						b.Fatal(err)
					}
					bb, err := blob.New(indexData, source)
					if err != nil {
						b.Fatal(err)
					}
					sinkBlob = bb
				}
			case formatEStargz:
				data := estargzData[format.name]
				server := newBenchHTTPServer(data, nil)
				defer server.Close()

				readerAt := &httpRangeReaderAt{
					client: client,
					url:    server.URL + "/data",
					size:   int64(len(data)),
				}

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					sr := io.NewSectionReader(readerAt, 0, int64(len(data)))
					r, err := estargz.Open(sr, format.estargzOpenOptions...)
					if err != nil {
						b.Fatal(err)
					}
					sinkReader = r
				}
			}
		})
	}
}

func BenchmarkCompareReadFile(b *testing.B) {
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
	formats := benchFormats()

	for _, bc := range cases {
		for _, pattern := range patterns {
			dir := b.TempDir()
			paths := makeBenchFiles(b, dir, bc.fileCount, bc.fileSize, pattern)

			blobData := make(map[blob.Compression]blobArchive)
			estargzData := make(map[string][]byte)

			for _, format := range formats {
				switch format.kind {
				case formatBlob:
					if _, ok := blobData[format.blobCompression]; !ok {
						indexData, dataData := buildBlobArchive(b, dir, format.blobCompression)
						blobData[format.blobCompression] = blobArchive{indexData: indexData, dataData: dataData}
					}
				case formatEStargz:
					if _, ok := estargzData[format.name]; !ok {
						estargzData[format.name] = buildEStargzArchive(b, dir, format.estargzOptions...)
					}
				}
			}

			for _, format := range formats {
				format := format
				b.Run(fmt.Sprintf("%s/%s/%s", bc.name, pattern, format.name), func(b *testing.B) {
					switch format.kind {
					case formatBlob:
						archive := blobData[format.blobCompression]
						for _, source := range benchSources() {
							source := source
							b.Run(source.name, func(b *testing.B) {
								byteSource, cleanup, err := source.newBlob(b, archive.dataData)
								if err != nil {
									b.Fatal(err)
								}
								if cleanup != nil {
									defer cleanup()
								}
								bb, err := blob.New(archive.indexData, byteSource)
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
									content, err := bb.ReadFile(path)
									if err != nil {
										b.Fatal(err)
									}
									sinkBytes = content
								}
							})
						}
					case formatEStargz:
						data := estargzData[format.name]
						for _, source := range benchSources() {
							source := source
							b.Run(source.name, func(b *testing.B) {
								readerAt, cleanup, err := source.newReaderAt(b, data)
								if err != nil {
									b.Fatal(err)
								}
								if cleanup != nil {
									defer cleanup()
								}
								sr := io.NewSectionReader(readerAt, 0, int64(len(data)))
								r, err := estargz.Open(sr, format.estargzOpenOptions...)
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
									fileReader, err := r.OpenFile(path)
									if err != nil {
										b.Fatal(err)
									}
									content, err := io.ReadAll(fileReader)
									if err != nil {
										b.Fatal(err)
									}
									sinkBytes = content
								}
							})
						}
					}
				})
			}
		}
	}
}

func BenchmarkCompareCopyDir(b *testing.B) {
	const (
		fileCount = 512
		fileSize  = 16 << 10
		prefix    = "dir00"
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)

	formats := benchFormats()
	blobData := make(map[blob.Compression]blobArchive)
	estargzData := make(map[string][]byte)

	for _, format := range formats {
		switch format.kind {
		case formatBlob:
			if _, ok := blobData[format.blobCompression]; !ok {
				indexData, dataData := buildBlobArchive(b, dir, format.blobCompression)
				blobData[format.blobCompression] = blobArchive{indexData: indexData, dataData: dataData}
			}
		case formatEStargz:
			if _, ok := estargzData[format.name]; !ok {
				estargzData[format.name] = buildEStargzArchive(b, dir, format.estargzOptions...)
			}
		}
	}

	dirEntries := countBenchDirEntries(fileCount, benchDirCount)
	totalBytes := int64(dirEntries * fileSize)

	for _, format := range formats {
		format := format
		b.Run(format.name, func(b *testing.B) {
			switch format.kind {
			case formatBlob:
				archive := blobData[format.blobCompression]
				for _, source := range benchSources() {
					source := source
					b.Run(source.name, func(b *testing.B) {
						byteSource, cleanup, err := source.newBlob(b, archive.dataData)
						if err != nil {
							b.Fatal(err)
						}
						if cleanup != nil {
							defer cleanup()
						}
						bb, err := blob.New(archive.indexData, byteSource)
						if err != nil {
							b.Fatal(err)
						}
						if totalBytes > 0 {
							b.SetBytes(totalBytes)
						}
						destRoot := b.TempDir()
						opts := []blob.CopyOption{blob.CopyWithCleanDest(true)}
						b.ReportAllocs()
						b.ResetTimer()
						for i := 0; b.Loop(); i++ {
							b.StopTimer()
							destDir := filepath.Join(destRoot, fmt.Sprintf("iter-%d", i))
							if err := os.MkdirAll(destDir, 0o755); err != nil {
								b.Fatal(err)
							}
							b.StartTimer()

							if err := bb.CopyDir(destDir, prefix, opts...); err != nil {
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
			case formatEStargz:
				data := estargzData[format.name]
				for _, source := range benchSources() {
					source := source
					b.Run(source.name, func(b *testing.B) {
						readerAt, cleanup, err := source.newReaderAt(b, data)
						if err != nil {
							b.Fatal(err)
						}
						if cleanup != nil {
							defer cleanup()
						}
						sr := io.NewSectionReader(readerAt, 0, int64(len(data)))
						r, err := estargz.Open(sr, format.estargzOpenOptions...)
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

							if err := copyDirEStargz(r, destDir, prefix); err != nil {
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
		})
	}
}

func BenchmarkCompareCopyAll(b *testing.B) {
	const (
		fileCount = 512
		fileSize  = 16 << 10
		prefix    = ""
	)

	dir := b.TempDir()
	makeBenchFiles(b, dir, fileCount, fileSize, benchPatternCompressible)

	formats := benchFormats()
	blobData := make(map[blob.Compression]blobArchive)
	estargzData := make(map[string][]byte)

	for _, format := range formats {
		switch format.kind {
		case formatBlob:
			if _, ok := blobData[format.blobCompression]; !ok {
				indexData, dataData := buildBlobArchive(b, dir, format.blobCompression)
				blobData[format.blobCompression] = blobArchive{indexData: indexData, dataData: dataData}
			}
		case formatEStargz:
			if _, ok := estargzData[format.name]; !ok {
				estargzData[format.name] = buildEStargzArchive(b, dir, format.estargzOptions...)
			}
		}
	}

	totalBytes := int64(fileCount * fileSize)

	for _, format := range formats {
		format := format
		b.Run(format.name, func(b *testing.B) {
			switch format.kind {
			case formatBlob:
				archive := blobData[format.blobCompression]
				for _, source := range benchSources() {
					source := source
					b.Run(source.name, func(b *testing.B) {
						byteSource, cleanup, err := source.newBlob(b, archive.dataData)
						if err != nil {
							b.Fatal(err)
						}
						if cleanup != nil {
							defer cleanup()
						}
						bb, err := blob.New(archive.indexData, byteSource)
						if err != nil {
							b.Fatal(err)
						}
						if totalBytes > 0 {
							b.SetBytes(totalBytes)
						}
						destRoot := b.TempDir()
						opts := []blob.CopyOption{blob.CopyWithCleanDest(true)}
						b.ReportAllocs()
						b.ResetTimer()
						for i := 0; b.Loop(); i++ {
							b.StopTimer()
							destDir := filepath.Join(destRoot, fmt.Sprintf("iter-%d", i))
							if err := os.MkdirAll(destDir, 0o755); err != nil {
								b.Fatal(err)
							}
							b.StartTimer()

							if err := bb.CopyDir(destDir, prefix, opts...); err != nil {
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
			case formatEStargz:
				data := estargzData[format.name]
				for _, source := range benchSources() {
					source := source
					b.Run(source.name, func(b *testing.B) {
						readerAt, cleanup, err := source.newReaderAt(b, data)
						if err != nil {
							b.Fatal(err)
						}
						if cleanup != nil {
							defer cleanup()
						}
						sr := io.NewSectionReader(readerAt, 0, int64(len(data)))
						r, err := estargz.Open(sr, format.estargzOpenOptions...)
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

							if err := copyDirEStargz(r, destDir, prefix); err != nil {
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
		})
	}
}

func copyDirEStargz(r *estargz.Reader, destDir, prefix string) error {
	entry, ok := r.Lookup(prefix)
	if !ok {
		return &fs.PathError{Op: "copydir", Path: prefix, Err: fs.ErrNotExist}
	}
	if entry.Type != "dir" {
		return &fs.PathError{Op: "copydir", Path: prefix, Err: errors.New("not a directory")}
	}
	return copyEStargzEntry(r, entry, prefix, destDir)
}

func copyEStargzEntry(r *estargz.Reader, entry *estargz.TOCEntry, entryPath, destDir string) error {
	destPath := filepath.Join(destDir, filepath.FromSlash(entryPath))
	switch entry.Type {
	case "dir":
		if err := os.MkdirAll(destPath, 0o755); err != nil {
			return err
		}
		var childErr error
		entry.ForeachChild(func(name string, child *estargz.TOCEntry) bool {
			if childErr != nil {
				return false
			}
			childPath := path.Join(entryPath, name)
			childErr = copyEStargzEntry(r, child, childPath, destDir)
			return childErr == nil
		})
		return childErr
	case "reg":
		reader, err := r.OpenFile(entryPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		f, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, reader); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	default:
		return fmt.Errorf("unsupported entry type %q for %s", entry.Type, entryPath)
	}
}

func makeBenchFiles(b *testing.B, dir string, fileCount, fileSize int, pattern benchPattern) []string {
	b.Helper()

	paths := make([]string, 0, fileCount)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < fileCount; i++ {
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

func buildBlobArchive(b *testing.B, dir string, compression blob.Compression) (indexData, dataData []byte) {
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

func buildEStargzArchive(b *testing.B, dir string, opts ...estargz.Option) []byte {
	b.Helper()

	tarData := buildTarFromDir(b, dir)
	sr := io.NewSectionReader(bytes.NewReader(tarData), 0, int64(len(tarData)))
	rc, err := estargz.Build(sr, opts...)
	if err != nil {
		b.Fatal(err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
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

func newBenchHTTPServer(data []byte, index []byte) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	})
	if index != nil {
		mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "index", time.Time{}, bytes.NewReader(index))
		})
	}
	return httptest.NewServer(mux)
}

func fetchHTTPBytes(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

type memByteSource struct {
	data     []byte
	sourceID string
}

func (m *memByteSource) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
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

type benchSource struct {
	name        string
	newBlob     func(b *testing.B, data []byte) (blob.ByteSource, func(), error)
	newReaderAt func(b *testing.B, data []byte) (io.ReaderAt, func(), error)
}

func benchSources() []benchSource {
	sources := []benchSource{
		{
			name:        "source=mem",
			newBlob:     newBenchMemBlobSource,
			newReaderAt: newBenchMemReaderAt,
		},
	}
	if benchHTTPEnabled() {
		sources = append(sources, benchSource{
			name:        "source=http",
			newBlob:     newBenchHTTPBlobSource,
			newReaderAt: newBenchHTTPReaderAt,
		})
	}
	return sources
}

func newBenchMemBlobSource(_ *testing.B, data []byte) (blob.ByteSource, func(), error) {
	return &memByteSource{data: data, sourceID: "mem"}, nil, nil
}

func newBenchMemReaderAt(_ *testing.B, data []byte) (io.ReaderAt, func(), error) {
	return bytes.NewReader(data), nil, nil
}

func benchHTTPEnabled() bool {
	return os.Getenv("BLOB_BENCH_HTTP") != ""
}

func newBenchHTTPBlobSource(_ *testing.B, data []byte) (blob.ByteSource, func(), error) {
	cfg, err := benchHTTPConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	client := benchHTTPClient(cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	}))
	src, err := blobhttp.NewSource(server.URL, blobhttp.WithClient(client))
	if err != nil {
		server.Close()
		return nil, nil, err
	}
	return src, server.Close, nil
}

func newBenchHTTPReaderAt(_ *testing.B, data []byte) (io.ReaderAt, func(), error) {
	cfg, err := benchHTTPConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	client := benchHTTPClient(cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "data", time.Time{}, bytes.NewReader(data))
	}))
	readerAt := &httpRangeReaderAt{
		client: client,
		url:    server.URL,
		size:   int64(len(data)),
	}
	return readerAt, server.Close, nil
}

type httpRangeReaderAt struct {
	client *http.Client
	url    string
	size   int64
}

func (r *httpRangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}
	end := off + int64(len(p)) - 1
	if end >= r.size {
		end = r.size - 1
	}
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %s", resp.Status)
	}
	want := int(end - off + 1)
	n, err := io.ReadFull(resp.Body, p[:want])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return n, err
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

type benchHTTPConfig struct {
	latency        time.Duration
	bytesPerSecond int64
}

func benchHTTPClient(cfg benchHTTPConfig) *http.Client {
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
	return &http.Client{Transport: transport}
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
	base           http.RoundTripper
	latency        time.Duration
	bytesPerSecond int64
}

func (rt *benchHTTPRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
