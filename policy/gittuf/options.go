package gittuf

import (
	"log/slog"
	"time"
)

// PolicyOption configures a Policy.
type PolicyOption func(*Policy) error

// WithRepository sets the source repository URL to verify.
// This is required for the policy to function.
func WithRepository(url string) PolicyOption {
	return func(p *Policy) error {
		p.repoURL = url
		return nil
	}
}

// WithLogger sets a custom logger for the policy.
func WithLogger(logger *slog.Logger) PolicyOption {
	return func(p *Policy) error {
		p.logger = logger
		return nil
	}
}

// WithCacheDir overrides the default cache directory for cloned repositories.
// Default: ~/.cache/blob/gittuf
func WithCacheDir(path string) PolicyOption {
	return func(p *Policy) error {
		p.cache = NewRepositoryCache(
			WithCacheBaseDir(path),
			WithCacheMaxEntries(p.cache.maxEntries),
			WithCacheTTLOption(p.cache.ttl),
		)
		return nil
	}
}

// WithCacheTTL sets the cache time-to-live for cloned repositories.
// Repositories older than the TTL will be refreshed on next use.
// Default: 1 hour
func WithCacheTTL(ttl time.Duration) PolicyOption {
	return func(p *Policy) error {
		p.cache.ttl = ttl
		return nil
	}
}

// WithFullVerification enables full RSL history verification.
// By default, only the latest RSL entry is verified (faster).
// Full verification walks the entire RSL chain from the beginning.
func WithFullVerification() PolicyOption {
	return func(p *Policy) error {
		p.latestOnly = false
		return nil
	}
}

// WithOverrideRef specifies the git ref to verify, overriding the ref
// extracted from SLSA provenance. Use this when the provenance ref
// doesn't match the ref you want to verify.
func WithOverrideRef(refName string) PolicyOption {
	return func(p *Policy) error {
		p.overrideRef = refName
		return nil
	}
}

// WithAllowMissingGittuf allows verification to pass if the source
// repository does not have gittuf enabled. This enables gradual adoption
// of gittuf across repositories.
// Default: false (verification fails if gittuf is not present)
func WithAllowMissingGittuf() PolicyOption {
	return func(p *Policy) error {
		p.allowMissingGittuf = true
		return nil
	}
}

// WithAllowMissingProvenance allows verification to pass if no SLSA
// provenance is found in the artifact. Use with caution - this effectively
// bypasses source provenance verification when provenance is absent.
// Default: false (verification fails if provenance is missing)
func WithAllowMissingProvenance() PolicyOption {
	return func(p *Policy) error {
		p.allowMissingProvenance = true
		return nil
	}
}
