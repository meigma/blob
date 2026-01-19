package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/meigma/blob/client"
	blob "github.com/meigma/blob/core"
)

type pushConfig struct {
	ref       string
	assets    string
	plainHTTP bool
}

func runPush(args []string) error {
	cfg := pushConfig{
		assets: "./assets",
	}

	fs := flag.NewFlagSet("push", flag.ExitOnError)
	fs.StringVar(&cfg.ref, "ref", "", "OCI reference with tag (required)")
	fs.StringVar(&cfg.assets, "assets", cfg.assets, "directory to archive")
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

	// Create archive with zstd compression
	var indexBuf, dataBuf bytes.Buffer
	err = blob.Create(ctx, cfg.assets, &indexBuf, &dataBuf,
		blob.CreateWithCompression(blob.CompressionZstd),
	)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	fmt.Printf("Archive created: index=%d bytes, data=%d bytes\n",
		indexBuf.Len(), dataBuf.Len())

	// Create blob from buffers for push
	// We need a ByteSource implementation for the data buffer
	dataBytes := dataBuf.Bytes()
	b, err := blob.New(indexBuf.Bytes(), &memSource{data: dataBytes})
	if err != nil {
		return fmt.Errorf("load archive: %w", err)
	}

	// Create client with appropriate options
	var opts []client.Option
	opts = append(opts, client.WithDockerConfig())
	if cfg.plainHTTP {
		opts = append(opts, client.WithPlainHTTP(true))
	}
	c := client.New(opts...)

	fmt.Printf("Pushing to %s...\n", cfg.ref)

	err = c.Push(ctx, cfg.ref, b)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}

	// Fetch to get the digest
	manifest, err := c.Fetch(ctx, cfg.ref)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}

	fmt.Printf("Pushed successfully!\n")
	fmt.Printf("Digest: %s\n", manifest.Digest())
	fmt.Printf("\nTo pull this archive:\n")
	fmt.Printf("  provenance pull --ref %s\n", cfg.ref)

	return nil
}

// memSource implements blob.ByteSource for in-memory data.
type memSource struct {
	data []byte
}

func (m *memSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, fmt.Errorf("offset %d exceeds data length %d", off, len(m.data))
	}
	n := copy(p, m.data[off:])
	return n, nil
}

func (m *memSource) Size() int64 {
	return int64(len(m.data))
}

func (m *memSource) SourceID() string {
	return "memory"
}
