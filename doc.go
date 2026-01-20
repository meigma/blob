// Package blob provides a file archive format optimized for random access
// via HTTP range requests against OCI registries.
//
// This package provides a unified high-level API through [Client] for pushing
// and pulling blob archives to/from OCI registries. For low-level archive
// operations without registry interaction, use the [core] subpackage.
//
// Archives consist of two OCI blobs:
//   - Index blob: FlatBuffers-encoded file metadata enabling O(log n) lookups
//   - Data blob: Concatenated file contents, sorted by path for efficient directory fetches
//
// # Quick Start
//
// Push a directory to a registry:
//
//	c, err := blob.NewClient(blob.WithDockerConfig())
//	if err != nil {
//	    return err
//	}
//	err = c.Push(ctx, "ghcr.io/myorg/myarchive:v1", "./src",
//	    blob.PushWithCompression(blob.CompressionZstd),
//	)
//
// Pull and read files:
//
//	archive, err := c.Pull(ctx, "ghcr.io/myorg/myarchive:v1")
//	if err != nil {
//	    return err
//	}
//	content, err := archive.ReadFile("config.json")
//
// # Caching
//
// Use WithCacheDir for automatic caching of all blob metadata and content:
//
//	c, err := blob.NewClient(
//	    blob.WithDockerConfig(),
//	    blob.WithCacheDir("/var/cache/blob"),
//	)
//
// For fine-grained control, use individual cache options like
// [WithRefCacheDir], [WithManifestCacheDir], [WithContentCacheDir], etc.
//
// # Policies
//
// Add verification policies to enforce security requirements on pull:
//
//	sigPolicy, _ := sigstore.NewPolicy(sigstore.WithIdentity(issuer, subject))
//	c, err := blob.NewClient(
//	    blob.WithDockerConfig(),
//	    blob.WithPolicy(sigPolicy),
//	)
package blob
