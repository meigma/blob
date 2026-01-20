package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/meigma/blob"
	"github.com/meigma/blob/policy"
	"github.com/meigma/blob/policy/sigstore"
	"github.com/meigma/blob/policy/slsa"
)

type pullConfig struct {
	ref        string
	output     string
	repo       string
	skipSig    bool
	skipAttest bool
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
	opts := []blob.Option{blob.WithDockerConfig()}
	if cfg.plainHTTP {
		opts = append(opts, blob.WithPlainHTTP(true))
	}

	// Build policies if not skipped
	var policies []blob.Policy

	// Add sigstore policy if not skipped
	if !cfg.skipSig {
		sigPolicy, err := buildSigstorePolicy(cfg.repo)
		if err != nil {
			return nil, err
		}
		policies = append(policies, sigPolicy)
	} else {
		fmt.Println("Skipping signature verification")
	}

	// Add SLSA policy if not skipped
	if !cfg.skipAttest {
		slsaPolicy, err := buildSLSAPolicy(cfg.repo)
		if err != nil {
			return nil, err
		}
		policies = append(policies, slsaPolicy)
	} else {
		fmt.Println("Skipping attestation policy")
	}

	// Combine policies with RequireAll
	if len(policies) > 0 {
		combined := policy.RequireAll(policies...)
		opts = append(opts, blob.WithPolicy(combined))
	}

	return opts, nil
}

// buildSigstorePolicy creates the sigstore verification policy for GitHub Actions.
func buildSigstorePolicy(repo string) (*sigstore.Policy, error) {
	fmt.Printf("Configuring signature verification for %s\n", repo)
	return sigstore.GitHubActionsPolicy(repo)
}

// buildSLSAPolicy creates the SLSA provenance policy for GitHub Actions.
func buildSLSAPolicy(repo string) (*slsa.Policy, error) {
	fmt.Printf("Configuring SLSA provenance verification for %s\n", repo)
	return slsa.GitHubActionsWorkflow(repo)
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
