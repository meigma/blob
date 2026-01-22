package registry

import blob "github.com/meigma/blob/core"

// PushOption configures a Push operation.
type PushOption func(*pushConfig)

type pushConfig struct {
	tags        []string
	annotations map[string]string
	progress    blob.ProgressFunc
}

// WithTags applies additional tags to the pushed manifest.
//
// The primary tag from the ref is always applied. These tags are applied
// after the initial push succeeds.
func WithTags(tags ...string) PushOption {
	return func(cfg *pushConfig) {
		cfg.tags = append(cfg.tags, tags...)
	}
}

// WithAnnotations sets custom annotations on the manifest.
//
// Standard annotations like org.opencontainers.image.created are set
// automatically and can be overridden.
func WithAnnotations(annotations map[string]string) PushOption {
	return func(cfg *pushConfig) {
		if cfg.annotations == nil {
			cfg.annotations = make(map[string]string)
		}
		for k, v := range annotations {
			cfg.annotations[k] = v
		}
	}
}

// WithProgress sets a callback to receive progress updates during push.
// The callback receives events for index and data blob uploads.
// The callback may be invoked concurrently and must be safe for concurrent use.
func WithProgress(fn blob.ProgressFunc) PushOption {
	return func(cfg *pushConfig) {
		cfg.progress = fn
	}
}
