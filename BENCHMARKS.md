# Benchmarking and Profiling

This repo includes Go benchmarks under package `blob` (see `*_test.go` files).
The harness generates synthetic datasets on disk, builds an archive, and then
measures core operations:

- Writer creation (compressed and uncompressed)
- Index lookups and prefix scans
- Reader `ReadFile` (compressed and uncompressed, compressible and random data)
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
