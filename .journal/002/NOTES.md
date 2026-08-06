---
id: 002
title: New session
started: 2026-08-05
---

## 2026-08-05 19:17 — Kickoff
Goal for the session: Start a new journal session and wait for the user's substantive request.
Current state of the world: The personal journal worktree and required session files are present, and no substantive task has been requested yet.
Plan: Bind this session to the current task, then proceed iteratively once the user provides the goal.

## 2026-08-05 20:45 — Large OS image architecture analysis
Goal: Determine whether Blob can efficiently store and transfer 5–10 GB OS image files, and identify the smallest changes needed if it cannot.

Target: Reviewed the clean `origin/master` tree at `964f55dab21feed76cdb34101af13cda8ffc4524`; the developer checkout was dirty and nine commits behind, so it was not used as the code target.

Findings:
- The archive and OCI representation can address 5–10 GB files without a format migration: archive offsets/sizes are `uint64`, OCI sizes are `int64`, and a one-file uncompressed artifact has a tiny index plus an image-sized data blob.
- The root `Client.Push` buffers the complete archive in memory and is unsuitable. `core.CreateBlob` plus `PushArchive` is constant-heap but creates a full scratch copy, then ORAS v2.6.2 performs one monolithic PUT with no existence preflight, byte progress, chunking, or resume.
- Pull defaults reject files larger than 256 MiB. `ReadFile` allocates the whole file; `CopyTo`/`CopyDir` allocate each complete contiguous data group; `CopyFile` is bounded-memory but turns typical 32 KiB reads into one HTTP Range request each.
- A disposable 8 MiB exact-tree probe measured 256 Range GETs for `CopyFile`, 1,024 for `Open` plus `io.Copy`, and one request for a direct `ReadRange`. The `CopyFile` behavior extrapolates to about 163,840 requests at 5 GiB and 327,680 at 10 GiB.
- Default disk content caching is harmful for large files: it downloads the whole entry before checking the 100 MiB capacity, discards an oversized entry, then the caller downloads it again. The 8 MiB undersized-cache probe transferred 16 MiB.
- Lazy HTTP reads discard caller cancellation by using `context.Background()`. Closing a partially read compressed range drains the rest; an 8 MiB probe read 1 KiB and then transferred the complete 8 MiB during close even with verify-on-close disabled.
- Range support is a target-registry capability, not an OCI guarantee. Registry blob-size and timeout limits are also provider-specific.

Validation:
- `go test ./...` passed at the exact target.
- Focused `registry:2` integration tests for push, pull, lazy loading, maximum file size, and chunked reads passed under the race detector.
- A focused 62.5 MiB archive-creation benchmark allocated 136,356,448 B/op; existing nominal large integration fixtures are only about 100 KiB and do not validate multi-gigabyte behavior.

Decision: The current architecture is format-compatible but not production-ready for this use case. Preserve the two-layer format. Add narrow streaming `PushFile` and context-aware/resumable `FetchFile` paths, large-file-aware cache bypass, digest existence checks, resumable OCI chunk uploads, and byte progress. Use one uncompressed packaged image (preferably already space-efficient, such as QCOW2) for the first target-registry spike. Defer multi-layer chunking, sparse extents, and format changes until measurements justify them.

Next: Present the evidence-backed verdict and agile implementation/validation sequence; no product code was changed in this analysis.
