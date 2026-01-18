---
sidebar_position: 2
---

# Integrity Model

Understanding how blob ensures file integrity.

## Per-File SHA256 Hashes

Every file entry in a blob archive stores the SHA256 hash of its uncompressed content. This hash serves multiple purposes that interlock to provide comprehensive integrity guarantees.

### Why Hash Each File?

The most obvious purpose is verification. After fetching file content via a range request, the client can compute the SHA256 hash of the received data and compare it against the stored hash. If they match, the content is correct. If they differ, something went wrong: network corruption, storage bitrot, or intentional tampering.

Per-file hashes enable precise identification of problems. When verification fails, the client knows exactly which file is affected. There is no ambiguity about whether the corruption is in file A or file B; the failing hash identifies the problematic file directly.

These hashes also enable content-addressed caching. The hash uniquely identifies the file's content regardless of its path, the archive it came from, or when it was created. Two files with identical content have identical hashes, which means the cache can store them once and serve both. This deduplication happens automatically without any explicit coordination.

### Why SHA256?

SHA256 provides a good balance of security, performance, and ubiquity. It is cryptographically strong enough to detect accidental corruption and resist intentional collision attacks. It is fast enough that hashing does not dominate file access time, especially for the file sizes blob typically handles. And it is universally supported across languages, platforms, and tools.

The choice is also conservative. SHA256 has years of analysis behind it and remains the standard for content addressing in systems like Git, OCI registries, and content-delivery networks. Using a less common hash algorithm would provide minimal benefit while complicating interoperability.

### Hashing Uncompressed Content

Blob hashes the uncompressed content, not the compressed bytes. This means the hash identifies what the file contains, not how it happens to be stored. If an archive is rebuilt with different compression settings, files with unchanged content retain unchanged hashes.

This choice has important implications for caching. A file compressed with zstd at level 3 has the same hash as the same file compressed at level 19, or stored uncompressed. The cache does not care about compression; it cares about content. Different compression choices do not fragment the cache or cause redundant storage.

## Verification Timing

Blob provides multiple points where verification can occur, each with different trade-offs between safety and performance.

### During Streaming Reads

When reading a file through the `Open` interface, blob computes the hash incrementally as data is read. Each `Read` call processes bytes through a hashing reader that updates the hash state. This approach avoids buffering the entire file in memory just for verification.

The hash is checked when the stream reaches EOF. If the computed hash matches the stored hash, the read completes normally. If they differ, the final read returns `ErrHashMismatch`. This means callers must read to EOF to trigger verification; stopping partway through a file means skipping verification.

### On Close

When a file opened via `Open` is closed, blob can optionally verify the hash even if the caller did not read to EOF. This behavior is controlled by the `WithVerifyOnClose` option, which is enabled by default.

If verification on close is enabled and the caller closes the file without reading all content, `Close` drains the remaining data through the hasher and verifies the result. This ensures that even callers who read only part of a file still get verification, at the cost of reading data they did not need.

If verification on close is disabled, `Close` simply releases resources without additional verification. This is appropriate when callers know they will always read to EOF, or when they accept the risk of unverified partial reads.

### On ReadFile

The `ReadFile` method reads an entire file into memory and verifies its hash before returning. There is no streaming variant; the caller receives verified content or an error. This is the simplest usage pattern and provides the strongest guarantees: if `ReadFile` returns successfully, the content is correct.

### Partial Reads

Callers who read only part of a file may receive unverified data. The hash covers the entire file, so verification requires reading the entire file. A caller who reads the first 100 bytes of a 10MB file has no way to verify those 100 bytes without also reading the remaining bytes.

This is an inherent limitation of whole-file hashing. Partial verification would require a different hash structure, like Merkle trees with per-chunk hashes. Blob chose simplicity over this capability because the target use cases typically read entire files.

## Cache Hit Semantics

Content-addressed caching fundamentally changes verification semantics. When the cache returns content for a given hash, that content is implicitly verified.

### Why Cache Hits Skip Verification

The cache lookup key is the hash itself. If the cache returns content for hash X, that content must have hash X by definition. Re-computing the hash would produce the same answer because the lookup already established what the content is.

This is not merely an optimization. It reflects the semantics of content-addressed storage: the address (hash) defines the content. Asking "does this content match its hash?" is equivalent to asking "is X equal to X?" The answer is yes by definition.

### Trust in the Cache

This reasoning depends on trusting the cache implementation. A cache that returns incorrect content for a given hash would violate the invariant that enables skipping verification. Blob's cache interface does not mandate any particular implementation, so the trust assumption falls on whichever cache is configured.

The disk cache implementation stores files named by their hash, so filesystem integrity protects cache integrity. If the filesystem corrupts a cached file, subsequent reads may return incorrect content, but this would require filesystem-level failures that are rare on modern systems.

For deployments with strict integrity requirements, caches could implement additional safeguards like checksums in metadata or periodic validation. These are not required by the interface but could be added by specific implementations.

### Cache Population

When a file is read from the source (not from cache), blob verifies its hash before caching. The cache receives only verified content. This ensures that a corrupted fetch does not poison the cache with bad data.

The verification-before-caching guarantee means cache entries are trustworthy as long as the initial population was correct. Combined with the content-addressed lookup, this provides end-to-end integrity from source through cache to application.

## OCI Integration

Blob's integrity model integrates with OCI's existing infrastructure to provide a complete chain of trust from source to consumption.

:::tip Practical Guide
For step-by-step instructions on implementing signature verification and provenance policies, see the [Provenance & Signing](../guides/provenance) guide.
:::

### The Integrity Chain

```
Signature -> Manifest -> Index Digest -> Per-file hashes
                      -> Data Digest
```

At the top of the chain is an optional signature on the OCI manifest. Tools like cosign or notation can sign manifests, cryptographically binding them to a specific signer's identity. Verification checks that the manifest was signed by an expected key.

The manifest contains digests for both the index blob and the data blob. Anyone who trusts the manifest (via signature verification or other means) can verify that they received the correct blobs by comparing their computed digests against the manifest's recorded digests.

The index blob contains per-file hashes. Anyone who trusts the index (via the manifest digest) can verify individual files by comparing their content hashes against the index entries.

This chain means tampering at any level invalidates the entire chain. Modifying a file changes its hash, which breaks verification against the index. Modifying the index changes its digest, which breaks verification against the manifest. Modifying the manifest invalidates its signature.

### Sigstore Signing

Blob includes built-in support for Sigstore signature verification via the `policy/sigstore` package. Sigstore provides keyless signing using OIDC identity tokens, which is particularly useful for CI/CD pipelines like GitHub Actions.

When pulling an archive with a Sigstore policy configured, the client:

1. Fetches the manifest from the registry
2. Queries for Sigstore bundle referrers attached to the manifest
3. Verifies the bundle against the Sigstore public good instance (or a custom trusted root)
4. Optionally validates that the signer matches an expected identity (issuer + subject)

If verification fails, the pull is rejected before any file content is fetched.

### SLSA Provenance

The OCI referrers API allows attaching attestations to artifacts. SLSA provenance attestations can describe how an archive was built: what inputs went in, what build system produced it, and what controls were in place. These attestations attach to the manifest as referrers.

Blob includes an OPA-based policy engine (`policy/opa`) for validating these attestations. Policies are written in Rego and can enforce requirements such as:

- Builds must come from specific GitHub organizations
- Only certain CI/CD workflows are trusted
- Specific builder identities are required

Consumers can query referrers to discover attestations and verify that an archive meets their provenance requirements. The integrity chain ensures that verified provenance applies to the actual content received, not just to some abstract artifact identity.

### Why Not Embed Signatures?

Some formats embed signatures within the archive itself. Blob delegates signing to OCI's existing infrastructure because that infrastructure already exists, is well-understood, and integrates with existing tooling. Reinventing signature embedding would duplicate effort and fragment the ecosystem.

## Why Not Merkle Trees?

Merkle trees are a common structure for integrity verification in distributed systems. They enable compact proofs that a specific piece of data belongs to a larger dataset. Given a Merkle tree, one can prove a file is part of an archive without transmitting the entire index.

Blob uses simple per-file hashes instead of Merkle trees. This choice prioritizes simplicity for the current use case.

### When Merkle Trees Help

Merkle trees shine when you need to prove membership without revealing the full dataset. In blockchain systems, they prove that a transaction was included in a block. In certificate transparency, they prove that a certificate was logged. In both cases, verifiers receive compact proofs rather than full datasets.

For blob's use case, compact membership proofs are not necessary. The verifier already has the full index (fetched once, cached locally). Checking whether a file belongs to the archive requires only looking it up in the index. The index is small enough to fetch completely.

### The Simplicity Trade-off

Merkle trees add complexity. Building them requires constructing the tree structure. Verifying them requires understanding tree traversal and proof verification. Debugging failures requires reasoning about tree levels and sibling hashes.

Per-file hashes are simple. Each file has a hash. Verify by comparing. Debug by examining two values. The mental model fits in one sentence.

### OCI Signing Provides Tamper Protection

One purpose Merkle trees serve is tamper detection: proving that nobody modified the dataset after the tree was computed. For blob, OCI signing already provides this guarantee. The manifest signature protects both blobs, and the blob digests protect their contents.

Adding Merkle trees for tamper protection would duplicate what OCI signing already provides. The additional complexity would not add additional security.

### Future Extensibility

The design does not preclude adding Merkle trees later if requirements change. The index format can evolve to include tree structure alongside or instead of flat hashes. Existing archives would remain readable; new archives could use the enhanced structure.

For now, simple per-file hashes meet the requirements with minimal complexity.

## Error Handling

When integrity verification fails, blob returns `ErrHashMismatch`. Understanding how to handle this error helps build robust applications.

### Detection at Read Time

Hash mismatches are detected when content is read, not when files are opened. Opening a file does not fetch or verify content; it only prepares for reading. The verification happens during `Read` calls (at EOF) or during `ReadFile`.

This means error handling must happen at read sites, not open sites. Code that opens files and passes them to other code without reading cannot detect integrity failures. The code that ultimately reads the content receives the error.

### Recovery Strategies

When `ErrHashMismatch` occurs, the content is corrupt. The cached copy (if any) is wrong, and the data received from the source was wrong. Recovery requires fetching fresh data.

For cached content, the appropriate response is usually to evict the cache entry and retry. The corrupt entry should not remain in the cache to cause future failures. The retry fetches from the source, verifies, and repopulates the cache with correct content.

For uncached content (direct from source), the appropriate response depends on whether the corruption is persistent or transient. Transient corruption (network glitch, temporary registry issue) may resolve on retry. Persistent corruption (bitrot in the registry, intentional tampering) will not resolve by retrying the same source.

Applications should implement retry with backoff for transient failures, but should not retry indefinitely. If corruption persists, escalate to alerting or fail with a clear error message identifying the affected file.

### Distinguishing Corruption Sources

`ErrHashMismatch` does not distinguish between corruption in transit, corruption in storage, or intentional tampering. All produce the same symptom: content does not match its hash. Additional investigation may be needed to identify root causes.

Comparing digests at different points can help narrow down where corruption occurred. If the manifest digest matches but a file hash does not, the corruption is within the data blob. If the manifest digest does not match, the corruption is at the blob level or higher. Registry logs and network traces may provide additional diagnostic information.

For detailed API documentation on error types and methods, see the [API reference](../reference/api.md).
