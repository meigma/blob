//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/meigma/blob"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// --- Registry Container Setup ---

var (
	registryOnce sync.Once
	registryAddr string
	registryErr  error
)

// getRegistry returns the shared registry address, starting the container if needed.
// The container is shared across all tests for performance.
func getRegistry(tb testing.TB) string {
	tb.Helper()

	if os.Getenv("SKIP_DOCKER_TESTS") == "1" {
		tb.Skip("SKIP_DOCKER_TESTS is set")
	}

	registryOnce.Do(func() {
		ctx := context.Background()
		registryAddr, registryErr = startRegistryContainer(ctx)
	})

	if registryErr != nil {
		tb.Fatalf("start registry container: %v", registryErr)
	}

	return registryAddr
}

// startRegistryContainer starts a registry:2 container and returns the host:port address.
func startRegistryContainer(ctx context.Context) (string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "registry:2",
		ExposedPorts: []string{"5000/tcp"},
		WaitingFor:   wait.ForHTTP("/v2/").WithPort("5000/tcp").WithStatusCodeMatcher(isOKStatus),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return "", fmt.Errorf("start registry container: %w", err)
	}

	// Note: Container cleanup is handled by testcontainers Reaper.
	// For explicit cleanup, you can add a cleanup function.

	host, err := container.Host(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve registry host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5000/tcp")
	if err != nil {
		return "", fmt.Errorf("resolve registry port: %w", err)
	}

	return fmt.Sprintf("%s:%s", host, port.Port()), nil
}

func isOKStatus(status int) bool {
	return status >= 200 && status < 300
}

// --- Test Client Factory ---

// newTestClient creates a client configured for the local test registry.
func newTestClient(tb testing.TB, registryAddr string, opts ...blob.Option) *blob.Client {
	tb.Helper()

	// Always use plain HTTP for local registry
	allOpts := append([]blob.Option{blob.WithPlainHTTP(true)}, opts...)

	client, err := blob.NewClient(allOpts...)
	require.NoError(tb, err, "create test client")

	return client
}

// --- Test Reference Helpers ---

// testRef generates a unique reference for a test to avoid collisions.
func testRef(registryAddr, testName string) string {
	return fmt.Sprintf("%s/test/%s:latest", registryAddr, testName)
}

// testRefWithTag generates a reference with a specific tag.
func testRefWithTag(registryAddr, testName, tag string) string {
	return fmt.Sprintf("%s/test/%s:%s", registryAddr, testName, tag)
}

// --- Test Data Helpers ---

// createTestFiles writes test files to a directory.
func createTestFiles(tb testing.TB, dir string, files map[string][]byte) {
	tb.Helper()
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		require.NoError(tb, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(tb, os.WriteFile(fullPath, content, 0o644))
	}
}

// makeCompressibleContent creates content that benefits from compression.
func makeCompressibleContent(size int) []byte {
	pattern := []byte("This is a repeating pattern for compression testing. ")
	result := make([]byte, 0, size)
	for len(result) < size {
		result = append(result, pattern...)
	}
	return result[:size]
}

// makeRandomContent creates random binary content.
func makeRandomContent(size int) []byte {
	data := make([]byte, size)
	_, _ = rand.Read(data)
	return data
}

// --- Standard Test Fixtures ---

// smallArchive is a simple flat archive with 3 small files.
var smallArchive = map[string][]byte{
	"hello.txt":   []byte("Hello, World!"),
	"readme.md":   []byte("# Test Archive\n\nThis is a test."),
	"config.json": []byte(`{"version": 1, "name": "test"}`),
}

// nestedArchive contains nested directories.
var nestedArchive = map[string][]byte{
	"root.txt":          []byte("root file"),
	"dir1/a.txt":        []byte("file a in dir1"),
	"dir1/b.txt":        []byte("file b in dir1"),
	"dir1/sub/c.txt":    []byte("file c in dir1/sub"),
	"dir2/x.txt":        []byte("file x in dir2"),
	"dir2/deep/y.txt":   []byte("file y in dir2/deep"),
	"dir2/deep/z.txt":   []byte("file z in dir2/deep"),
	"empty/placeholder": []byte(""),
}

// compressibleArchive contains files that benefit significantly from compression.
var compressibleArchive = map[string][]byte{
	"large.txt":     makeCompressibleContent(100 * 1024), // 100KB
	"small.txt":     []byte("tiny"),
	"repeated.json": []byte(`{"data": "` + string(makeCompressibleContent(10*1024)) + `"}`),
}

// --- Assertion Helpers ---

// assertFilesMatch verifies that an archive contains the expected files with correct content.
func assertFilesMatch(tb testing.TB, archive *blob.Archive, expected map[string][]byte) {
	tb.Helper()

	for path, expectedContent := range expected {
		path = filepath.ToSlash(path)
		gotContent, err := archive.ReadFile(path)
		require.NoError(tb, err, "ReadFile(%q)", path)
		require.Equal(tb, expectedContent, gotContent, "content mismatch for %q", path)
	}
}

// assertDirContents verifies that a directory contains the expected files with correct content.
func assertDirContents(tb testing.TB, dir string, expected map[string][]byte) {
	tb.Helper()

	for path, expectedContent := range expected {
		fullPath := filepath.Join(dir, path)
		gotContent, err := os.ReadFile(fullPath)
		require.NoError(tb, err, "ReadFile(%q)", fullPath)
		require.Equal(tb, expectedContent, gotContent, "content mismatch for %q", path)
	}
}

// assertArchiveLen verifies the number of entries in an archive.
func assertArchiveLen(tb testing.TB, archive *blob.Archive, expected int) {
	tb.Helper()
	require.Equal(tb, expected, archive.Len(), "archive entry count")
}

// extractNames returns the names from directory entries.
func extractNames(entries []fs.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
