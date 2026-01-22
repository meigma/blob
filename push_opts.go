package blob

import blobcore "github.com/meigma/blob/core"

// PushOption configures a Push or PushArchive operation.
type PushOption func(*pushConfig)

type pushConfig struct {
	tags        []string
	annotations map[string]string
	createOpts  []blobcore.CreateOption
	progress    ProgressFunc
}

// PushWithTags applies additional tags to the pushed manifest.
//
// The primary tag from the ref is always applied. These tags are applied
// after the initial push succeeds.
func PushWithTags(tags ...string) PushOption {
	return func(cfg *pushConfig) {
		cfg.tags = append(cfg.tags, tags...)
	}
}

// PushWithAnnotations sets custom annotations on the manifest.
//
// Standard annotations like org.opencontainers.image.created are set
// automatically and can be overridden.
func PushWithAnnotations(annotations map[string]string) PushOption {
	return func(cfg *pushConfig) {
		if cfg.annotations == nil {
			cfg.annotations = make(map[string]string)
		}
		for k, v := range annotations {
			cfg.annotations[k] = v
		}
	}
}

// --- Archive creation options (for Push, not PushArchive) ---

// PushWithCompression sets the compression algorithm for archive creation.
// Use [CompressionNone] to store files uncompressed, [CompressionZstd] for zstd.
func PushWithCompression(c Compression) PushOption {
	return func(cfg *pushConfig) {
		cfg.createOpts = append(cfg.createOpts, blobcore.CreateWithCompression(c))
	}
}

// PushWithSkipCompression adds predicates that decide to store a file uncompressed.
// If any predicate returns true, compression is skipped for that file.
func PushWithSkipCompression(fns ...SkipCompressionFunc) PushOption {
	return func(cfg *pushConfig) {
		cfg.createOpts = append(cfg.createOpts, blobcore.CreateWithSkipCompression(fns...))
	}
}

// PushWithChangeDetection controls whether the writer verifies files did not change
// during archive creation.
func PushWithChangeDetection(cd ChangeDetection) PushOption {
	return func(cfg *pushConfig) {
		cfg.createOpts = append(cfg.createOpts, blobcore.CreateWithChangeDetection(cd))
	}
}

// PushWithMaxFiles limits the number of files included in the archive.
// Zero uses DefaultMaxFiles. Negative means no limit.
func PushWithMaxFiles(n int) PushOption {
	return func(cfg *pushConfig) {
		cfg.createOpts = append(cfg.createOpts, blobcore.CreateWithMaxFiles(n))
	}
}

// PushWithProgress sets a callback to receive progress updates during push.
// The callback receives events for archive creation (compressing files) and
// blob uploads (pushing index and data).
// The callback may be invoked concurrently and must be safe for concurrent use.
func PushWithProgress(fn ProgressFunc) PushOption {
	return func(cfg *pushConfig) {
		cfg.progress = fn
		cfg.createOpts = append(cfg.createOpts, blobcore.CreateWithProgress(fn))
	}
}
