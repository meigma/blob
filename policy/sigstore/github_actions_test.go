package sigstore

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "main",
			input:   "main",
			want:    true,
		},
		{
			name:    "exact no match",
			pattern: "main",
			input:   "master",
			want:    false,
		},
		{
			name:    "wildcard suffix",
			pattern: "v*",
			input:   "v1.0.0",
			want:    true,
		},
		{
			name:    "wildcard suffix no match",
			pattern: "v*",
			input:   "release-1.0",
			want:    false,
		},
		{
			name:    "wildcard middle",
			pattern: "release/*",
			input:   "release/v1",
			want:    true,
		},
		{
			name:    "wildcard does not cross slashes",
			pattern: "release/*",
			input:   "release/v1/hotfix",
			want:    false,
		},
		{
			name:    "multiple wildcards",
			pattern: "release/*/hotfix-*",
			input:   "release/v1/hotfix-123",
			want:    true,
		},
		{
			name:    "escape regex special chars",
			pattern: "v1.0.0",
			input:   "v1.0.0",
			want:    true,
		},
		{
			name:    "escape regex special chars no match",
			pattern: "v1.0.0",
			input:   "v1x0x0",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regex := globToRegex(tt.pattern)
			re := regexp.MustCompile("^" + regex + "$")
			got := re.MatchString(tt.input)
			assert.Equal(t, tt.want, got, "pattern=%q regex=%q input=%q", tt.pattern, regex, tt.input)
		})
	}
}

func TestBuildGitHubActionsSubjectRegex(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		cfg      *gitHubActionsConfig
		subjects []string
		wantErr  bool
	}{
		{
			name: "any ref",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/main",
				"https://github.com/myorg/myrepo/.github/workflows/ci.yaml@refs/tags/v1.0.0",
				"https://github.com/myorg/myrepo/.github/workflows/test.yml@refs/pull/123/merge",
			},
		},
		{
			name: "specific branch",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{branches: []string{"main"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/main",
			},
		},
		{
			name: "branch wildcard",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{branches: []string{"release/*"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/release/v1",
				"https://github.com/myorg/myrepo/.github/workflows/ci.yml@refs/heads/release/v2",
			},
		},
		{
			name: "tag wildcard",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{tags: []string{"v*"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/tags/v1.0.0",
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/tags/v2.0.0-beta",
			},
		},
		{
			name: "branches and tags",
			repo: "myorg/myrepo",
			cfg: &gitHubActionsConfig{
				branches: []string{"main"},
				tags:     []string{"v*"},
			},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/main",
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/tags/v1.0.0",
			},
		},
		{
			name: "arbitrary refs",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{refs: []string{"refs/pull/*/merge"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/ci.yml@refs/pull/123/merge",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := buildGitHubActionsSubjectRegex(tt.repo, tt.cfg)
			re, err := regexp.Compile(pattern)
			require.NoError(t, err, "pattern should compile: %s", pattern)

			for _, subject := range tt.subjects {
				assert.True(t, re.MatchString(subject),
					"pattern=%q should match subject=%q", pattern, subject)
			}
		})
	}
}

func TestBuildGitHubActionsSubjectRegex_NoMatch(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		cfg      *gitHubActionsConfig
		subjects []string
	}{
		{
			name: "wrong repo",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{},
			subjects: []string{
				"https://github.com/other/repo/.github/workflows/release.yml@refs/heads/main",
			},
		},
		{
			name: "branch restriction excludes tags",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{branches: []string{"main"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/tags/v1.0.0",
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/develop",
			},
		},
		{
			name: "tag restriction excludes branches",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{tags: []string{"v*"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/main",
			},
		},
		{
			name: "wildcard does not cross path segments",
			repo: "myorg/myrepo",
			cfg:  &gitHubActionsConfig{branches: []string{"release/*"}},
			subjects: []string{
				"https://github.com/myorg/myrepo/.github/workflows/release.yml@refs/heads/release/v1/hotfix",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := buildGitHubActionsSubjectRegex(tt.repo, tt.cfg)
			re, err := regexp.Compile(pattern)
			require.NoError(t, err, "pattern should compile: %s", pattern)

			for _, subject := range tt.subjects {
				assert.False(t, re.MatchString(subject),
					"pattern=%q should NOT match subject=%q", pattern, subject)
			}
		})
	}
}

func TestGitHubActionsPolicy_EmptyRepo(t *testing.T) {
	_, err := GitHubActionsPolicy("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repository cannot be empty")
}

func TestGitHubActionsPolicy_ValidRepo(t *testing.T) {
	// This test verifies the policy can be created without network calls failing
	// The actual signature verification would require a real registry with signatures
	policy, err := GitHubActionsPolicy("myorg/myrepo")
	require.NoError(t, err)
	assert.NotNil(t, policy)
	assert.NotNil(t, policy.identity)
}

func TestGitHubActionsPolicy_WithOptions(t *testing.T) {
	policy, err := GitHubActionsPolicy("myorg/myrepo",
		AllowBranches("main", "develop"),
		AllowTags("v*"),
	)
	require.NoError(t, err)
	assert.NotNil(t, policy)
}
