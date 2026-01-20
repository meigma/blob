package blob

import (
	"bytes"
	"context"
	"fmt"

	blobcore "github.com/meigma/blob/core"
	"github.com/meigma/blob/registry"
)

// Push creates an archive from srcDir and pushes it to the registry.
//
// This is the most common operation - create + push in one call.
// The ref must include a tag (e.g., "registry.com/repo:v1.0.0").
//
// Use [PushWithTags] to apply additional tags to the same manifest.
// Use [PushWithCompression] to configure compression (default: none).
func (c *Client) Push(ctx context.Context, ref, srcDir string, opts ...PushOption) error {
	cfg := pushConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Create archive in memory
	var indexBuf, dataBuf bytes.Buffer
	createOpts := cfg.createOpts
	if err := blobcore.Create(ctx, srcDir, &indexBuf, &dataBuf, createOpts...); err != nil {
		return fmt.Errorf("create archive: %w", err)
	}

	// Create Blob from buffers
	archive, err := blobcore.New(indexBuf.Bytes(), &memSource{data: dataBuf.Bytes()})
	if err != nil {
		return fmt.Errorf("load archive: %w", err)
	}

	// Push via registry client
	return c.pushArchive(ctx, ref, archive, &cfg)
}

// PushArchive pushes an existing archive to the registry.
//
// Use when you have a pre-created archive (e.g., from [blobcore.CreateBlob]).
// The ref must include a tag (e.g., "registry.com/repo:v1.0.0").
//
// Use [PushWithTags] to apply additional tags to the same manifest.
func (c *Client) PushArchive(ctx context.Context, ref string, archive *blobcore.Blob, opts ...PushOption) error {
	cfg := pushConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return c.pushArchive(ctx, ref, archive, &cfg)
}

// pushArchive is the internal push implementation.
func (c *Client) pushArchive(ctx context.Context, ref string, archive *blobcore.Blob, cfg *pushConfig) error {
	// Build registry client options
	var regOpts []registry.Option //nolint:prealloc // size depends on optional config
	regOpts = append(regOpts, registry.WithOrasOptions(c.orasOpts...))
	if c.refCache != nil {
		regOpts = append(regOpts, registry.WithRefCache(c.refCache))
	}
	if c.manifestCache != nil {
		regOpts = append(regOpts, registry.WithManifestCache(c.manifestCache))
	}
	if c.indexCache != nil {
		regOpts = append(regOpts, registry.WithIndexCache(c.indexCache))
	}
	for _, p := range c.policies {
		regOpts = append(regOpts, registry.WithPolicy(p))
	}

	regClient := registry.New(regOpts...)

	// Build push options
	var pushOpts []registry.PushOption
	if len(cfg.tags) > 0 {
		pushOpts = append(pushOpts, registry.WithTags(cfg.tags...))
	}
	if cfg.annotations != nil {
		pushOpts = append(pushOpts, registry.WithAnnotations(cfg.annotations))
	}

	return regClient.Push(ctx, ref, archive, pushOpts...)
}

// memSource implements blobcore.ByteSource for in-memory data.
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
