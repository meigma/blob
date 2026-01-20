// Package gittuf provides source provenance verification using gittuf.
//
// This package implements [github.com/meigma/blob/registry.Policy] to verify
// that source code changes were authorized according to gittuf policies. While
// SLSA provenance proves how an archive was built, gittuf proves whether the
// source changes were authorized by the repository's security policy.
//
// # Separate Module
//
// This package is a separate Go module (github.com/meigma/blob/policy/gittuf)
// to isolate the gittuf dependency from the core blob library.
//
// # How It Works
//
// The policy extracts source information (repository URL and git ref) from
// SLSA provenance attestations, then verifies the ref against the source
// repository's gittuf Reference State Log (RSL). Verification confirms that
// all changes to the ref were made by authorized parties according to the
// repository's gittuf policy.
//
// # Basic Usage
//
// Create a policy for a GitHub repository:
//
//	policy, err := gittuf.GitHubRepository("myorg", "myrepo")
//
// Or specify a custom repository URL:
//
//	policy, err := gittuf.NewPolicy(
//	    gittuf.WithRepository("https://github.com/myorg/myrepo"),
//	)
//
// # Composition
//
// Combine with SLSA and Sigstore policies for complete provenance verification:
//
//	sigPolicy, _ := sigstore.GitHubActionsPolicy("myorg/myrepo")
//	slsaPolicy, _ := slsa.GitHubActionsWorkflow("myorg/myrepo")
//	gittufPolicy, _ := gittuf.GitHubRepository("myorg", "myrepo")
//
//	c, _ := blob.NewClient(
//	    blob.WithDockerConfig(),
//	    blob.WithPolicy(policy.RequireAll(sigPolicy, slsaPolicy, gittufPolicy)),
//	)
//
// This creates a complete trust chain:
//   - Sigstore: WHO signed the archive
//   - SLSA: HOW the archive was built
//   - Gittuf: WHETHER source changes were authorized
//
// # Gradual Adoption
//
// For repositories transitioning to gittuf, use [WithAllowMissingGittuf] to
// allow verification to pass when the source repository lacks gittuf metadata:
//
//	policy, err := gittuf.NewPolicy(
//	    gittuf.WithRepository("https://github.com/myorg/myrepo"),
//	    gittuf.WithAllowMissingGittuf(),
//	)
//
// # Caching
//
// The policy caches cloned repositories to avoid re-cloning on every
// verification. Configure the cache location and TTL:
//
//	policy, err := gittuf.NewPolicy(
//	    gittuf.WithRepository("https://github.com/myorg/myrepo"),
//	    gittuf.WithCacheDir("/var/cache/blob/gittuf"),
//	    gittuf.WithCacheTTL(2 * time.Hour),
//	)
//
// # Trust Model
//
// By default, the policy uses Trust On First Use (TOFU) for gittuf root keys.
// The first clone establishes the trusted root, and subsequent verifications
// ensure consistency with that root.
package gittuf
