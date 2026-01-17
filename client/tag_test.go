package client

import (
	"context"
	"errors"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Tag(t *testing.T) {
	t.Parallel()

	const (
		testRef    = "registry.example.com/repo:latest"
		testDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	)

	tests := []struct {
		name      string
		ref       string
		digest    string
		setupMock func(*mockOCIClient)
		wantErr   error
	}{
		{
			name:   "successful tag",
			ref:    testRef,
			digest: testDigest,
			setupMock: func(m *mockOCIClient) {
				m.TagFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
					assert.Equal(t, testRef, repoRef)
					assert.Equal(t, testDigest, desc.Digest.String())
					assert.Equal(t, "latest", tag)
					return nil
				}
			},
		},
		{
			name:    "invalid reference",
			ref:     "not a valid ref!!!",
			digest:  testDigest,
			wantErr: ErrInvalidReference,
		},
		{
			name:    "reference without tag",
			ref:     "registry.example.com/repo",
			digest:  testDigest,
			wantErr: ErrInvalidReference,
		},
		{
			name:    "reference with digest instead of tag",
			ref:     "registry.example.com/repo@sha256:abc123",
			digest:  testDigest,
			wantErr: ErrInvalidReference,
		},
		{
			name:    "invalid digest format",
			ref:     testRef,
			digest:  "not-a-valid-digest",
			wantErr: ErrInvalidReference,
		},
		{
			name:   "oci client error propagates",
			ref:    testRef,
			digest: testDigest,
			setupMock: func(m *mockOCIClient) {
				m.TagFunc = func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
					return errors.New("tag failed")
				}
			},
			wantErr: errors.New("tag failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockOCIClient{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			c := &Client{oci: mock}

			err := c.Tag(context.Background(), tt.ref, tt.digest)

			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, ErrInvalidReference) {
					assert.ErrorIs(t, err, ErrInvalidReference)
				} else {
					assert.Contains(t, err.Error(), tt.wantErr.Error())
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_Tag_PassesCorrectDescriptor(t *testing.T) {
	t.Parallel()

	const testDigest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	var capturedDesc *ocispec.Descriptor
	mock := &mockOCIClient{
		TagFunc: func(ctx context.Context, repoRef string, desc *ocispec.Descriptor, tag string) error {
			capturedDesc = desc
			return nil
		},
	}

	c := &Client{oci: mock}
	err := c.Tag(context.Background(), "registry.example.com/repo:v1.0.0", testDigest)

	require.NoError(t, err)
	require.NotNil(t, capturedDesc)
	assert.Equal(t, testDigest, capturedDesc.Digest.String())
}
