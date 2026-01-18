// Package main provides a CLI demonstrating blob archives with provenance verification.
//
// This example shows:
//   - Creating and pushing blob archives to OCI registries
//   - Pulling archives with sigstore signature verification
//   - Validating SLSA provenance attestations using OPA policies
//
// Usage:
//
//	provenance push --ref registry.example.com/repo:tag
//	provenance pull --ref registry.example.com/repo:tag
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "push":
		err = runPush(os.Args[2:])
	case "pull":
		err = runPull(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`provenance - blob archive provenance example

Usage:
  provenance <command> [options]

Commands:
  push    Create and push a blob archive to an OCI registry
  pull    Pull and verify a blob archive with policy enforcement

Push Options:
  --ref <reference>    OCI reference with tag (required)
  --assets <dir>       Directory to archive (default: ./assets)
  --plain-http         Use plain HTTP for local registries

Pull Options:
  --ref <reference>    OCI reference to pull (required)
  --output <dir>       Extraction directory (default: ./output)
  --policy <file>      OPA policy file (default: ./policy/policy.rego)
  --issuer <url>       Expected OIDC issuer (default: GitHub Actions)
  --subject <id>       Expected signing identity (optional)
  --skip-sig           Skip signature verification
  --skip-attest        Skip attestation policy
  --plain-http         Use plain HTTP for local registries

Examples:
  # Push to ttl.sh (anonymous, temporary)
  provenance push --ref ttl.sh/my-archive:1h

  # Pull without verification (local testing)
  provenance pull --ref ttl.sh/my-archive:1h --skip-sig --skip-attest

  # Pull with full verification (CI artifacts)
  provenance pull --ref ghcr.io/myorg/archive:v1 --policy ./policy/policy.rego`)
}
