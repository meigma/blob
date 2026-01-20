package registry

// FetchOption configures a Fetch operation.
type FetchOption func(*fetchConfig)

type fetchConfig struct {
	skipCache bool
}

// WithSkipCache bypasses the manifest cache for this fetch.
//
// The fetched manifest is still added to the cache after retrieval.
func WithSkipCache() FetchOption {
	return func(cfg *fetchConfig) {
		cfg.skipCache = true
	}
}
