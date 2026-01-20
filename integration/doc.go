//go:build integration

// Package integration provides integration tests for the blob library.
//
// These tests require Docker and spin up a real OCI registry using testcontainers.
// Run with: go test -tags=integration ./integration/...
package integration
