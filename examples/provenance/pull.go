package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/meigma/blob"
	"github.com/meigma/blob/client"
	"github.com/meigma/blob/policy/opa"
	"github.com/meigma/blob/policy/sigstore"
)

// Default GitHub Actions OIDC issuer.
const defaultIssuer = "https://token.actions.githubusercontent.com"

type pullConfig struct {
	ref        string
	output     string
	policy     string
	issuer     string
	subject    string
	skipSig    bool
	skipAttest bool
	plainHTTP  bool
}

func runPull(args []string) error {
	cfg := pullConfig{
		output: "./output",
		policy: "./policy/policy.rego",
		issuer: defaultIssuer,
	}

	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	fs.StringVar(&cfg.ref, "ref", "", "OCI reference to pull (required)")
	fs.StringVar(&cfg.output, "output", cfg.output, "extraction directory")
	fs.StringVar(&cfg.policy, "policy", cfg.policy, "OPA policy file")
	fs.StringVar(&cfg.issuer, "issuer", cfg.issuer, "expected OIDC issuer")
	fs.StringVar(&cfg.subject, "subject", "", "expected signing identity")
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
	c := client.New(opts...)

	fmt.Printf("Pulling %s...\n", cfg.ref)

	b, err := c.Pull(ctx, cfg.ref)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	// Close the underlying source when done
	defer func() {
		if closer, ok := b.Reader().Source().(io.Closer); ok {
			closer.Close()
		}
	}()

	return extractArchive(b, cfg.output)
}

// buildClientOptions configures client policies based on pullConfig.
func buildClientOptions(cfg *pullConfig) ([]client.Option, error) {
	opts := []client.Option{client.WithDockerConfig()}
	if cfg.plainHTTP {
		opts = append(opts, client.WithPlainHTTP(true))
	}

	// Add sigstore policy if not skipped
	if !cfg.skipSig {
		sigPolicy, err := buildSigstorePolicy(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, client.WithPolicy(sigPolicy))
	} else {
		fmt.Println("Skipping signature verification")
	}

	// Add OPA policy if not skipped
	if !cfg.skipAttest {
		opaPolicy, err := buildOPAPolicy(cfg.policy)
		if err != nil {
			return nil, err
		}
		opts = append(opts, client.WithPolicy(opaPolicy))
	} else {
		fmt.Println("Skipping attestation policy")
	}

	return opts, nil
}

// buildSigstorePolicy creates the sigstore verification policy.
func buildSigstorePolicy(cfg *pullConfig) (*sigstore.Policy, error) {
	fmt.Printf("Configuring signature verification (issuer: %s)\n", cfg.issuer)
	var sigOpts []sigstore.PolicyOption
	if cfg.subject != "" {
		sigOpts = append(sigOpts, sigstore.WithIdentity(cfg.issuer, cfg.subject))
		fmt.Printf("  Subject: %s\n", cfg.subject)
	}
	policy, err := sigstore.NewPolicy(sigOpts...)
	if err != nil {
		return nil, fmt.Errorf("create sigstore policy: %w", err)
	}
	return policy, nil
}

// buildOPAPolicy creates the OPA attestation policy from a file.
func buildOPAPolicy(policyPath string) (*opa.Policy, error) {
	if _, err := os.Stat(policyPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("policy file not found: %s (use --skip-attest to skip)", policyPath)
		}
		return nil, fmt.Errorf("policy file: %w", err)
	}

	fmt.Printf("Loading attestation policy from %s\n", policyPath)
	// Use Sigstore bundle artifact type for GitHub attestations
	policy, err := opa.NewPolicy(
		opa.WithPolicyFile(policyPath),
		opa.WithArtifactType(opa.SigstoreBundleArtifactType),
	)
	if err != nil {
		return nil, fmt.Errorf("create OPA policy: %w", err)
	}
	return policy, nil
}

// extractArchive extracts the blob archive to the output directory.
func extractArchive(b *blob.Blob, output string) error {
	entryCount := b.Len()
	fmt.Printf("Archive contains %d files\n", entryCount)

	if err := os.MkdirAll(output, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	fmt.Printf("Extracting to %s...\n", output)

	err := b.CopyDir(output, "",
		blob.CopyWithPreserveMode(true),
		blob.CopyWithPreserveTimes(true),
		blob.CopyWithCleanDest(true),
	)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	fmt.Printf("Extracted %d files to %s\n", entryCount, output)
	fmt.Println("Verification successful!")

	return nil
}
