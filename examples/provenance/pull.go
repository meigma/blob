package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/meigma/blob"
	"github.com/meigma/blob/policy"
	"github.com/meigma/blob/policy/gittuf"
	"github.com/meigma/blob/policy/sigstore"
	"github.com/meigma/blob/policy/slsa"
)

type pullConfig struct {
	ref        string
	output     string
	repo       string
	skipSig    bool
	skipAttest bool
	skipGittuf bool
	plainHTTP  bool
}

func runPull(args []string) error {
	cfg := pullConfig{
		output: "./output",
		repo:   "meigma/blob",
	}

	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	fs.StringVar(&cfg.ref, "ref", "", "OCI reference to pull (required)")
	fs.StringVar(&cfg.output, "output", cfg.output, "extraction directory")
	fs.StringVar(&cfg.repo, "repo", cfg.repo, "GitHub repository (owner/repo)")
	fs.BoolVar(&cfg.skipSig, "skip-sig", false, "skip signature verification")
	fs.BoolVar(&cfg.skipAttest, "skip-attest", false, "skip attestation policy")
	fs.BoolVar(&cfg.skipGittuf, "skip-gittuf", false, "skip gittuf source verification")
	fs.BoolVar(&cfg.plainHTTP, "plain-http", false, "use plain HTTP")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if cfg.ref == "" {
		return errors.New("--ref is required")
	}

	return pull(&cfg)
}

func pull(cfg *pullConfig) error {
	ctx := context.Background()

	// Build client with policies
	opts, err := buildClientOptions(cfg)
	if err != nil {
		return err
	}

	client, err := blob.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	fmt.Printf("Pulling %s...\n", cfg.ref)

	archive, err := client.Pull(ctx, cfg.ref)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	// Close the underlying source when done
	defer func() {
		if closer, ok := archive.Reader().Source().(io.Closer); ok {
			closer.Close()
		}
	}()

	return extractArchive(archive, cfg.output)
}

// buildClientOptions configures client policies based on pullConfig.
func buildClientOptions(cfg *pullConfig) ([]blob.Option, error) {
	// Create debug logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	opts := []blob.Option{blob.WithDockerConfig(), blob.WithLogger(logger)}
	if cfg.plainHTTP {
		opts = append(opts, blob.WithPlainHTTP(true))
	}

	// Build policies if not skipped
	var policies []blob.Policy

	// Add sigstore policy if not skipped
	if !cfg.skipSig {
		sigPolicy, err := buildSigstorePolicy(cfg.repo, logger)
		if err != nil {
			return nil, err
		}
		policies = append(policies, sigPolicy)
	} else {
		fmt.Println("Skipping signature verification")
	}

	// Add SLSA policy if not skipped
	if !cfg.skipAttest {
		slsaPolicy, err := buildSLSAPolicy(cfg.repo, logger)
		if err != nil {
			return nil, err
		}
		policies = append(policies, slsaPolicy)
	} else {
		fmt.Println("Skipping attestation policy")
	}

	// Add gittuf source verification policy if not skipped
	if !cfg.skipGittuf {
		gittufPolicy, err := buildGittufPolicy(cfg.repo, logger)
		if err != nil {
			return nil, err
		}
		policies = append(policies, gittufPolicy)
	} else {
		fmt.Println("Skipping gittuf source verification")
	}

	// Combine policies with RequireAll
	if len(policies) > 0 {
		combined := policy.RequireAll(policies...)
		opts = append(opts, blob.WithPolicy(combined))
	}

	return opts, nil
}

// buildSigstorePolicy creates the sigstore verification policy for GitHub Actions.
func buildSigstorePolicy(repo string, logger *slog.Logger) (*sigstore.Policy, error) {
	fmt.Printf("Configuring signature verification for %s\n", repo)
	return sigstore.GitHubActionsPolicy(repo, sigstore.WithLogger(logger))
}

// buildSLSAPolicy creates the SLSA provenance policy for GitHub Actions.
func buildSLSAPolicy(repo string, logger *slog.Logger) (*slsa.Policy, error) {
	fmt.Printf("Configuring SLSA provenance verification for %s\n", repo)
	return slsa.GitHubActionsWorkflow(repo, slsa.WithLogger(logger))
}

// buildGittufPolicy creates the gittuf source verification policy.
// This verifies that source changes were authorized according to the
// repository's gittuf policy by checking the Reference State Log (RSL).
func buildGittufPolicy(repo string, logger *slog.Logger) (*gittuf.Policy, error) {
	fmt.Printf("Configuring gittuf source verification for %s\n", repo)

	// Parse owner/repo format
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository format %q, expected owner/repo", repo)
	}

	return gittuf.GitHubRepository(parts[0], parts[1],
		// Use latest-only verification for faster checks (default)
		// Full RSL history verification can be enabled with gittuf.WithFullVerification()

		// Allow graceful degradation when gittuf verification fails.
		// This is needed during gradual gittuf adoption because:
		// - Historical RSL entries may predate policy updates
		// - Clone-time verification of RSL history may fail
		// Remove this once gittuf is fully configured and all RSL entries verify.
		gittuf.WithAllowMissingGittuf(),

		// Allow missing SLSA provenance - gittuf extracts source info (repo, ref, commit)
		// from SLSA provenance. When provenance is missing, verification is skipped.
		// In production, use SLSA generators to create provenance attestations.
		gittuf.WithAllowMissingProvenance(),

		// Pass logger for detailed verification output
		gittuf.WithLogger(logger),
	)
}

// extractArchive extracts the blob archive to the output directory.
func extractArchive(archive *blob.Archive, output string) error {
	entryCount := archive.Len()
	fmt.Printf("Archive contains %d files\n", entryCount)

	if err := os.MkdirAll(output, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	fmt.Printf("Extracting to %s...\n", output)

	err := archive.CopyDir(output, "",
		blob.CopyWithPreserveMode(true),
		blob.CopyWithPreserveTimes(true),
		blob.CopyWithOverwrite(true),
	)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	fmt.Printf("Extracted %d files to %s\n", entryCount, output)
	fmt.Println("Verification successful!")

	return nil
}
