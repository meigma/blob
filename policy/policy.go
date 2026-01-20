// Package policy provides composition utilities for combining multiple policies.
//
// This package works with policies from the sigstore, slsa, and opa subpackages
// to create complex verification requirements without writing Rego.
//
// # Composition
//
// Use RequireAll for AND logic (all policies must pass):
//
//	combined := policy.RequireAll(
//	    sigstore.GitHubActionsPolicy("myorg/myrepo"),
//	    slsa.RequireBuilder("https://github.com/slsa-framework/slsa-github-generator"),
//	)
//
// Use RequireAny for OR logic (at least one policy must pass):
//
//	multiSource := policy.RequireAny(
//	    slsa.RequireSource("https://github.com/myorg/repo1"),
//	    slsa.RequireSource("https://github.com/myorg/repo2"),
//	)
//
// Compositions can be nested:
//
//	policy := policy.RequireAll(
//	    sigstorePolicy,
//	    policy.RequireAny(slsaPolicy1, slsaPolicy2),
//	)
package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/meigma/blob/registry"
)

// RequireAll returns a policy that passes only if all given policies pass.
//
// Policies are evaluated in order. Evaluation stops at the first failure.
// If no policies are provided, the returned policy always passes.
func RequireAll(policies ...registry.Policy) registry.Policy {
	return registry.PolicyFunc(func(ctx context.Context, req registry.PolicyRequest) error {
		for i, p := range policies {
			if p == nil {
				continue
			}
			if err := p.Evaluate(ctx, req); err != nil {
				return fmt.Errorf("policy %d: %w", i+1, err)
			}
		}
		return nil
	})
}

// RequireAny returns a policy that passes if at least one policy passes.
//
// All policies are evaluated until one succeeds. If all policies fail,
// the error includes messages from all failed policies.
// If no policies are provided, the returned policy fails with an error.
func RequireAny(policies ...registry.Policy) registry.Policy {
	return registry.PolicyFunc(func(ctx context.Context, req registry.PolicyRequest) error {
		// Filter nil policies
		var validPolicies []registry.Policy
		for _, p := range policies {
			if p != nil {
				validPolicies = append(validPolicies, p)
			}
		}

		if len(validPolicies) == 0 {
			return errors.New("policy: RequireAny requires at least one policy")
		}

		var errs []string
		for _, p := range validPolicies {
			if err := p.Evaluate(ctx, req); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			return nil // At least one passed
		}

		// All failed
		return fmt.Errorf("policy: all %d policies failed: %s",
			len(validPolicies), strings.Join(errs, "; "))
	})
}
