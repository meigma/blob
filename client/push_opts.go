package client

// PushOption configures a Push operation.
type PushOption func(*pushConfig)

type pushConfig struct {
	tags        []string
	annotations map[string]string
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
