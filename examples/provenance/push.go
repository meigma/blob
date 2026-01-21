package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/meigma/blob"
	"github.com/meigma/blob/policy/sigstore"
)

type pushConfig struct {
	ref       string
	assets    string
	sign      bool
	plainHTTP bool
}

func runPush(args []string) error {
	cfg := pushConfig{
		assets: "./assets",
	}

	fs := flag.NewFlagSet("push", flag.ExitOnError)
	fs.StringVar(&cfg.ref, "ref", "", "OCI reference with tag (required)")
	fs.StringVar(&cfg.assets, "assets", cfg.assets, "directory to archive")
	fs.BoolVar(&cfg.sign, "sign", false, "sign with sigstore (keyless, requires OIDC)")
	fs.BoolVar(&cfg.plainHTTP, "plain-http", false, "use plain HTTP")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if cfg.ref == "" {
		return errors.New("--ref is required")
	}

	return push(cfg)
}

func push(cfg pushConfig) error {
	ctx := context.Background()

	// Verify assets directory exists
	info, err := os.Stat(cfg.assets)
	if err != nil {
		return fmt.Errorf("assets directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", cfg.assets)
	}

	fmt.Printf("Creating archive from %s...\n", cfg.assets)

	// Create debug logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create client with appropriate options
	opts := []blob.Option{blob.WithDockerConfig(), blob.WithLogger(logger)}
	if cfg.plainHTTP {
		opts = append(opts, blob.WithPlainHTTP(true))
	}

	client, err := blob.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	fmt.Printf("Pushing to %s...\n", cfg.ref)

	err = client.Push(ctx, cfg.ref, cfg.assets, blob.PushWithCompression(blob.CompressionZstd))
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}

	// Fetch to get the digest
	manifest, err := client.Fetch(ctx, cfg.ref)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}

	fmt.Printf("Pushed successfully!\n")
	fmt.Printf("Digest: %s\n", manifest.Digest())

	// Sign if requested
	if cfg.sign {
		fmt.Println("Signing with sigstore (keyless)...")

		signer, err := sigstore.NewSigner(
			sigstore.WithEphemeralKey(),
			sigstore.WithFulcio("https://fulcio.sigstore.dev"),
			sigstore.WithRekor("https://rekor.sigstore.dev"),
			sigstore.WithAmbientCredentials(),
		)
		if err != nil {
			return fmt.Errorf("create signer: %w", err)
		}

		sigDigest, err := client.Sign(ctx, cfg.ref, signer)
		if err != nil {
			return fmt.Errorf("sign: %w", err)
		}

		fmt.Printf("Signed! Signature digest: %s\n", sigDigest)
	}

	fmt.Printf("\nTo pull this archive:\n")
	fmt.Printf("  provenance pull --ref %s\n", cfg.ref)

	return nil
}
