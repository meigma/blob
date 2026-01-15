# Benchmarking and Profiling

This repo includes Go benchmarks under package `blob` (see `*_test.go` files).
The harness generates synthetic datasets on disk, builds an archive, and then
measures core operations:

- Writer creation (compressed and uncompressed)
- Index lookups and prefix scans
- Reader `ReadFile` (compressed and uncompressed, compressible and random data)
- Reader `CopyDir` (directory extraction)
- Cached reader hits

## Run benchmarks

```bash
go test -run='^$' -bench=Benchmark -benchmem ./...
```

Recommended for stable results:

```bash
go test -run='^$' -bench=Benchmark -benchmem -count=10 > bench.txt
benchstat bench.txt
```

## Benchmark -> regression -> profiler flow

1) Capture a baseline and a change run, then compare with benchstat:

```bash
go test -run='^$' -bench=Benchmark -benchmem -count=10 > base.txt
go test -run='^$' -bench=Benchmark -benchmem -count=10 > change.txt
benchstat base.txt change.txt
```

2) Pick the benchmark that regressed and run the matching profiler mode with
the same dataset knobs. This gives you pprof/trace detail for the same path.

## Profiler harness

The `cmd/profiler` CLI mirrors the benchmarks and supports CPU/heap/trace
profiles plus long-running duration-based runs.

```bash
go run ./cmd/profiler -mode=readfile -files=64 -file-size=65536 -compression=zstd -pattern=random -duration=30s -cpuprofile=cpu.prof
go tool pprof -http=:8080 cpu.prof
```

## Benchmark -> profiler mapping

- `BenchmarkWriterCreate` → `-mode=writer`
- `BenchmarkIndexLookup` → `-mode=index-lookup`
- `BenchmarkIndexLookupCopy` → `-mode=index-lookup-copy`
- `BenchmarkEntriesWithPrefix` → `-mode=entries-with-prefix`
- `BenchmarkEntriesWithPrefixCopy` → `-mode=entries-with-prefix-copy`
- `BenchmarkReaderReadFile` → `-mode=readfile`
- `BenchmarkCachedReaderReadFileHit` → `-mode=cached-readfile-hit`
- `BenchmarkCachedReaderPrefetchDir` → `-mode=prefetchdir`
- `BenchmarkCachedReaderPrefetchDirDisk*` → `-mode=prefetchdir -cache=disk`
- `BenchmarkReaderCopyDir` → `-mode=copydir`

Common knobs:

- `-files`, `-file-size`, `-compression`, `-pattern` to match dataset sizes
- `-prefix` for prefix scans, prefetchdir, and copydir
- `-cache`, `-prefetch-workers`, `-prefetch-cold` for cache behaviors
- `-duration` or `-iterations` to control runtime

## CPU profiling

```bash
go test -run='^$' -bench=BenchmarkReaderReadFile -cpuprofile=cpu.prof -benchmem ./...
go tool pprof -http=:8080 cpu.prof
```

## Memory profiling

```bash
go test -run='^$' -bench=BenchmarkReaderReadFile -memprofile=mem.prof -benchmem ./...
go tool pprof -http=:8080 mem.prof
```

## Execution trace

```bash
go test -run='^$' -bench=BenchmarkReaderReadFile -trace=trace.out ./...
go tool trace trace.out
```

## Blocking and mutex profiling

Block/mutex profiles require explicit opt-in to avoid overhead during normal
runs:

```bash
BLOB_PROFILE_BLOCK=1 go test -run='^$' -bench=BenchmarkReaderReadFile -blockprofile=block.prof ./...
go tool pprof -http=:8080 block.prof
```

```bash
BLOB_PROFILE_MUTEX=1 go test -run='^$' -bench=BenchmarkReaderReadFile -mutexprofile=mutex.prof ./...
go tool pprof -http=:8080 mutex.prof
```

## Notes

- Use `-benchtime=3s` (or higher) if results are noisy.
- Run on an idle machine to avoid thermal throttling and background noise.
- For CPU scaling, try `-cpu=1,2,4,8`.

## Remote Benchmarking (Bare Metal)

For consistent, reproducible benchmark results, run on a dedicated bare metal
server using the `remote-bench.sh` script. This provisions a Latitude.sh server,
syncs code via Mutagen, and bootstraps a Go environment.

### Prerequisites

- [Latitude.sh CLI](https://github.com/latitudesh/lsh): `brew install latitudesh/tools/lsh`
- [Mutagen](https://mutagen.io): `brew install mutagen-io/mutagen/mutagen`
- `jq`: `brew install jq`
- Latitude.sh API key and project ID

### Setup

```bash
# Authenticate with Latitude.sh (one-time)
lsh login <YOUR_API_KEY>

# Set your project ID
export LATITUDE_PROJECT_ID="your-project-id"

# Provision server (~5-10 min for bare metal)
just bench-setup
```

### Running Benchmarks

```bash
# Run all benchmarks
just bench-remote

# Run specific benchmark with options
just bench-remote -bench=BenchmarkReaderCopyDir -count=10 -benchmem

# Compare baseline vs changes
just bench-remote -count=10 > base.txt
# ... make changes ...
just bench-remote -count=10 > change.txt
benchstat base.txt change.txt
```

### Other Commands

```bash
# Check environment status
just bench-status

# Open SSH session for debugging
./scripts/remote-bench.sh ssh

# Force code sync
./scripts/remote-bench.sh sync
```

### Teardown

```bash
# Destroy server and clean up
just bench-teardown
```

### Configuration

Override defaults via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `LATITUDE_PROJECT_ID` | (required) | Your Latitude.sh project ID |
| `LATITUDE_PLAN` | `c2-small-x86` | Server plan |
| `LATITUDE_SITE` | `LAX` | Datacenter (LAX, DAL, NYC, etc.) |
| `GO_VERSION` | `1.25.4` | Go version to install |
