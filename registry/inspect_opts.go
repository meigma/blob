package registry

// InspectOption configures an Inspect operation.
type InspectOption func(*inspectConfig)

type inspectConfig struct {
	skipCache    bool
	maxIndexSize int64
}

// WithInspectSkipCache bypasses the ref, manifest, and index caches.
//
// This forces a fresh fetch from the registry even if cached data exists.
func WithInspectSkipCache() InspectOption {
	return func(cfg *inspectConfig) {
		cfg.skipCache = true
	}
}

// WithInspectMaxIndexSize sets the maximum number of bytes allowed for the index blob.
//
// Use a value <= 0 to disable the limit.
func WithInspectMaxIndexSize(maxBytes int64) InspectOption {
	return func(cfg *inspectConfig) {
		cfg.maxIndexSize = maxBytes
	}
}
