package slsa

import (
	"log/slog"
	"regexp"
	"strings"
)

// PolicyOption configures a Policy.
type PolicyOption func(*Policy) error

// WithLogger sets a custom logger for the policy.
func WithLogger(logger *slog.Logger) PolicyOption {
	return func(p *Policy) error {
		p.logger = logger
		return nil
	}
}

// WithArtifactTypes sets the OCI artifact types to search for attestations.
// Defaults to both in-toto and Sigstore bundle types.
func WithArtifactTypes(types ...string) PolicyOption {
	return func(p *Policy) error {
		p.artifactTypes = types
		return nil
	}
}

// SourceOption configures RequireSource.
type SourceOption func(*sourceConfig)

type sourceConfig struct {
	repo        string
	ref         string
	refPatterns []*regexp.Regexp
}

// WithRef requires an exact ref match.
func WithRef(ref string) SourceOption {
	return func(c *sourceConfig) {
		c.ref = ref
	}
}

// WithBranches allows builds from specific branches.
// Branch names should NOT include the "refs/heads/" prefix.
// Supports simple wildcards: "release/*" matches "release/v1", etc.
func WithBranches(branches ...string) SourceOption {
	return func(c *sourceConfig) {
		for _, branch := range branches {
			pattern := "^refs/heads/" + globToRegex(branch) + "$"
			c.refPatterns = append(c.refPatterns, regexp.MustCompile(pattern))
		}
	}
}

// WithTags allows builds from specific tags.
// Tag names should NOT include the "refs/tags/" prefix.
// Supports simple wildcards: "v*" matches "v1.0.0", etc.
func WithTags(tags ...string) SourceOption {
	return func(c *sourceConfig) {
		for _, tag := range tags {
			pattern := "^refs/tags/" + globToRegex(tag) + "$"
			c.refPatterns = append(c.refPatterns, regexp.MustCompile(pattern))
		}
	}
}

// GitHubActionsWorkflowOption configures GitHubActionsWorkflow.
type GitHubActionsWorkflowOption func(*ghActionsConfig)

type ghActionsConfig struct {
	repo         string
	workflowPath string
	refPatterns  []*regexp.Regexp
}

// WithWorkflowPath requires the build to use a specific workflow file.
// The path should be relative to the repository root (e.g., ".github/workflows/release.yml").
func WithWorkflowPath(path string) GitHubActionsWorkflowOption {
	return func(c *ghActionsConfig) {
		c.workflowPath = path
	}
}

// WithWorkflowBranches allows builds from specific branches.
// Branch names should NOT include the "refs/heads/" prefix.
func WithWorkflowBranches(branches ...string) GitHubActionsWorkflowOption {
	return func(c *ghActionsConfig) {
		for _, branch := range branches {
			pattern := "^refs/heads/" + globToRegex(branch) + "$"
			c.refPatterns = append(c.refPatterns, regexp.MustCompile(pattern))
		}
	}
}

// WithWorkflowTags allows builds from specific tags.
// Tag names should NOT include the "refs/tags/" prefix.
func WithWorkflowTags(tags ...string) GitHubActionsWorkflowOption {
	return func(c *ghActionsConfig) {
		for _, tag := range tags {
			pattern := "^refs/tags/" + globToRegex(tag) + "$"
			c.refPatterns = append(c.refPatterns, regexp.MustCompile(pattern))
		}
	}
}

// globToRegex converts a simple glob pattern to a regex pattern.
// Only * is supported as a wildcard (matches any characters except /).
func globToRegex(pattern string) string {
	escaped := regexp.QuoteMeta(pattern)
	return strings.ReplaceAll(escaped, `\*`, `[^/]*`)
}
