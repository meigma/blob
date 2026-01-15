# Benchmarks

This directory stores canonical benchmark runs from the bare-metal server.

Canonical suite:
- BenchmarkWriterCreate
- BenchmarkIndexLookup
- BenchmarkIndexLookupCopy
- BenchmarkEntriesWithPrefix
- BenchmarkEntriesWithPrefixCopy
- BenchmarkReaderReadFile
- BenchmarkReaderCopyDir
- BenchmarkCachedReaderReadFileHit
- BenchmarkCachedReaderPrefetchDir
- BenchmarkCachedReaderPrefetchDirDisk
- BenchmarkCachedReaderPrefetchDirDiskParallel

Run:
- just bench-remote

File naming:
- bench_canonical-<UTCtimestamp>_<gitsha>[-dirty].txt
