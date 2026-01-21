package sigstore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/verify"
)

// GitHubActionsOIDCIssuer is the OIDC issuer for GitHub Actions.
const GitHubActionsOIDCIssuer = "https://token.actions.githubusercontent.com"

// GitHubActionsOption configures a GitHub Actions policy.
type GitHubActionsOption func(*gitHubActionsConfig)

type gitHubActionsConfig struct {
	branches []string // allowed branches (without refs/heads/ prefix)
	tags     []string // allowed tags (without refs/tags/ prefix)
	refs     []string // arbitrary refs (full path like refs/heads/main)
}

// AllowBranches restricts signatures to specific branches.
// Branch names should NOT include the "refs/heads/" prefix.
// Supports simple wildcards: "release/*" matches "release/v1", "release/v2", etc.
func AllowBranches(branches ...string) GitHubActionsOption {
	return func(c *gitHubActionsConfig) {
		c.branches = append(c.branches, branches...)
	}
}

// AllowTags restricts signatures to specific tags.
// Tag names should NOT include the "refs/tags/" prefix.
// Supports simple wildcards: "v*" matches "v1.0.0", "v2.0.0", etc.
func AllowTags(tags ...string) GitHubActionsOption {
	return func(c *gitHubActionsConfig) {
		c.tags = append(c.tags, tags...)
	}
}

// AllowRefs restricts signatures to arbitrary refs.
// Refs should include the full path (e.g., "refs/heads/main", "refs/pull/123/merge").
// Supports simple wildcards.
func AllowRefs(refs ...string) GitHubActionsOption {
	return func(c *gitHubActionsConfig) {
		c.refs = append(c.refs, refs...)
	}
}

// GitHubActionsPolicy creates a policy requiring signatures from GitHub Actions
// workflows in the specified repository.
//
// The repo parameter should be in "owner/repo" format (e.g., "myorg/myrepo").
// Do NOT include the "https://github.com/" prefix.
//
// By default, any workflow and any ref is allowed. Use [AllowBranches],
// [AllowTags], or [AllowRefs] to restrict which refs are accepted.
//
// Additional [PolicyOption] values (like [WithLogger]) can be passed after the
// GitHub Actions options.
//
// Example:
//
//	// Allow any ref from the repo
//	policy, err := sigstore.GitHubActionsPolicy("myorg/myrepo")
//
//	// Allow only main branch and release tags
//	policy, err := sigstore.GitHubActionsPolicy("myorg/myrepo",
//	    sigstore.AllowBranches("main"),
//	    sigstore.AllowTags("v*"),
//	)
func GitHubActionsPolicy(repo string, opts ...any) (*Policy, error) {
	if repo == "" {
		return nil, errors.New("sigstore: repository cannot be empty")
	}

	cfg := &gitHubActionsConfig{}
	var policyOpts []PolicyOption

	for _, opt := range opts {
		switch o := opt.(type) {
		case GitHubActionsOption:
			o(cfg)
		case PolicyOption:
			policyOpts = append(policyOpts, o)
		default:
			return nil, fmt.Errorf("sigstore: unexpected option type %T", opt)
		}
	}

	// Build the subject regex pattern
	subjectRegex := buildGitHubActionsSubjectRegex(repo, cfg)

	// Create identity with regex matching
	identity, err := verify.NewShortCertificateIdentity(
		GitHubActionsOIDCIssuer, "", // issuer exact, no issuer regex
		"", subjectRegex, // no subject exact, subject regex
	)
	if err != nil {
		return nil, fmt.Errorf("sigstore: create identity: %w", err)
	}

	// Add identity option first, then user-provided options
	allOpts := append([]PolicyOption{func(p *Policy) error {
		p.identity = &identity
		return nil
	}}, policyOpts...)

	// Fetch public Sigstore trusted root
	return NewPolicy(allOpts...)
}

// buildGitHubActionsSubjectRegex constructs the subject regex pattern.
// GitHub Actions subject format: https://github.com/OWNER/REPO/.github/workflows/WORKFLOW@REF
func buildGitHubActionsSubjectRegex(repo string, cfg *gitHubActionsConfig) string {
	// Escape repo for regex
	escapedRepo := regexp.QuoteMeta(repo)

	// Base pattern: any workflow from this repo
	workflowPattern := `.github/workflows/[^@]+`

	// Build ref pattern
	var refPattern string
	if len(cfg.branches) == 0 && len(cfg.tags) == 0 && len(cfg.refs) == 0 {
		// No restrictions - match any ref
		refPattern = `refs/.+`
	} else {
		// Build specific ref patterns
		patterns := make([]string, 0, len(cfg.branches)+len(cfg.tags)+len(cfg.refs))

		for _, branch := range cfg.branches {
			patterns = append(patterns, `refs/heads/`+globToRegex(branch))
		}
		for _, tag := range cfg.tags {
			patterns = append(patterns, `refs/tags/`+globToRegex(tag))
		}
		for _, ref := range cfg.refs {
			patterns = append(patterns, globToRegex(ref))
		}

		refPattern = `(?:` + strings.Join(patterns, `|`) + `)`
	}

	// Full pattern: https://github.com/OWNER/REPO/.github/workflows/WORKFLOW@REF
	return fmt.Sprintf(`^https://github\.com/%s/%s@%s$`,
		escapedRepo, workflowPattern, refPattern)
}

// globToRegex converts a simple glob pattern to a regex pattern.
// Only * is supported as a wildcard (matches any characters).
func globToRegex(pattern string) string {
	// Escape all regex special characters
	escaped := regexp.QuoteMeta(pattern)
	// Convert escaped \* back to .* for wildcard matching
	return strings.ReplaceAll(escaped, `\*`, `[^/]*`)
}
