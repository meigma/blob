package opa

import (
	"context"
	"log/slog"
	"os"

	"github.com/open-policy-agent/opa/v1/rego"
)

// PolicyOption configures a Policy.
type PolicyOption func(*Policy) error

// WithPolicy compiles inline Rego source code.
// The policy must define data.blob.policy.allow or data.blob.policy.deny rules.
func WithPolicy(regoSource string) PolicyOption {
	return func(p *Policy) error {
		query, err := rego.New(
			rego.Query("data.blob.policy"),
			rego.Module("policy.rego", regoSource),
		).PrepareForEval(context.Background())
		if err != nil {
			return err
		}
		p.query = &query
		return nil
	}
}

// WithPolicyFile loads and compiles a Rego policy from a file.
// The policy must define data.blob.policy.allow or data.blob.policy.deny rules.
func WithPolicyFile(path string) PolicyOption {
	return func(p *Policy) error {
		//nolint:gosec // path is intentionally user-provided for policy loading
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return WithPolicy(string(data))(p)
	}
}

// WithArtifactType sets the OCI artifact type to filter referrers.
// Defaults to "application/vnd.in-toto+json".
func WithArtifactType(artifactType string) PolicyOption {
	return func(p *Policy) error {
		p.artifactType = artifactType
		return nil
	}
}

// WithPredicateTypes sets the accepted in-toto predicate types.
// Only attestations with matching predicate types will be included in the input.
// Defaults to accepting SLSA provenance v1 and v0.2.
func WithPredicateTypes(types ...string) PolicyOption {
	return func(p *Policy) error {
		p.predicateTypes = types
		return nil
	}
}

// WithLogger sets a custom logger for the policy.
func WithLogger(logger *slog.Logger) PolicyOption {
	return func(p *Policy) error {
		p.logger = logger
		return nil
	}
}
