# Contributing to Blob

Thank you for your interest in contributing to Blob! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Commit Guidelines](#commit-guidelines)
- [Testing](#testing)
- [Pull Request Process](#pull-request-process)
- [Documentation](#documentation)
- [Getting Help](#getting-help)

## Code of Conduct

We are committed to providing a welcoming and inclusive environment. Please be respectful and constructive in all interactions. Harassment, discrimination, or abusive behavior will not be tolerated.

## Getting Started

### Prerequisites

- **Go 1.25+** - [Installation guide](https://go.dev/doc/install)
- **Docker** - Required for integration tests
- **just** - Command runner ([installation](https://github.com/casey/just#installation))
- **golangci-lint** - Go linter ([installation](https://golangci-lint.run/welcome/install/))
- **flatc** - FlatBuffers compiler ([releases](https://github.com/google/flatbuffers/releases))

### Setup

1. Fork the repository on GitHub

2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/blob.git
   cd blob
   ```

3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/meigma/blob.git
   ```

4. Verify your setup:
   ```bash
   just ci
   ```

## Development Workflow

### Creating a Branch

Create a feature branch from `master`:

```bash
git checkout master
git pull upstream master
git checkout -b feat/your-feature-name
```

Use descriptive branch names with prefixes like `feat/`, `fix/`, `docs/`, or `refactor/`.

### Available Commands

Use `just` to run common development tasks:

| Command | Description |
|---------|-------------|
| `just` | Run default checks (fmt, vet, lint, test) |
| `just ci` | Run full CI pipeline |
| `just build` | Build all packages |
| `just test` | Run unit tests with race detection |
| `just lint` | Run golangci-lint |
| `just fmt` | Check formatting |
| `just fmt-write` | Format code (modifies files) |
| `just generate` | Generate FlatBuffers code |
| `just tools` | Install development tools |

### Local Testing with a Registry

Start a local OCI registry for manual testing:

```bash
docker run -d -p 5000:5000 --name registry registry:2
```

## Code Style

### Formatting

Code must be formatted with `gofmt`:

```bash
just fmt        # Check formatting
just fmt-write  # Fix formatting
```

Imports must be organized in groups:
1. Standard library
2. External packages
3. Local packages (`github.com/meigma/blob`)

### Linting

All code must pass `golangci-lint` with no errors:

```bash
just lint
```

The linter enforces:
- **Error handling** - All errors must be explicitly handled
- **Security** - No common security vulnerabilities (gosec)
- **Code quality** - Various best practices via revive, gocritic, etc.

### Best Practices

- Keep functions focused and concise
- Use meaningful variable and function names
- Add godoc comments for exported functions and types
- Handle errors explicitly; avoid ignoring them
- Use `context.Context` for cancellation and timeouts

## Commit Guidelines

This project uses [Conventional Commits](https://www.conventionalcommits.org/) for automated versioning and changelog generation.

### Format

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Types and Version Impact

| Type | Version Bump | Example |
|------|--------------|---------|
| `fix:` | Patch (0.0.x) | `fix: handle nil pointer in registry client` |
| `feat:` | Minor (0.x.0) | `feat: add zstd compression support` |
| `feat!:` | Major (x.0.0) | `feat!: redesign Client API` |
| `BREAKING CHANGE:` | Major (x.0.0) | Footer indicating breaking change |

Other types (no version bump, but tracked in changelog):
- `docs:` - Documentation changes
- `chore:` - Maintenance tasks
- `test:` - Test additions or fixes
- `ci:` - CI/CD changes
- `refactor:` - Code refactoring
- `style:` - Code style changes
- `perf:` - Performance improvements

### Examples

```bash
# Bug fix
git commit -m "fix: prevent panic when manifest is nil"

# New feature
git commit -m "feat: add directory prefetching"

# Breaking change
git commit -m "feat!: change Push signature to accept options struct"

# With scope
git commit -m "fix(cache): handle concurrent cache writes"

# With body
git commit -m "feat: add content-addressed caching

Implements local filesystem caching with automatic
deduplication across archives based on content hash."
```

## Testing

### Unit Tests

Run unit tests:

```bash
just test
```

Tests run with race detection and coverage enabled by default.

### Writing Tests

- Use [testify](https://github.com/stretchr/testify) for assertions
- Place test files alongside source files (`foo_test.go`)
- Use table-driven tests for multiple test cases
- Tag integration tests with `//go:build integration`

Example:

```go
func TestBlob_ReadFile(t *testing.T) {
    tests := []struct {
        name    string
        path    string
        want    string
        wantErr bool
    }{
        // test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

## Pull Request Process

### Before Submitting

1. **Sync with upstream**:
   ```bash
   git fetch upstream
   git rebase upstream/master
   ```

2. **Run all checks**:
   ```bash
   just ci
   ```

### Submitting a PR

1. Push your branch to your fork:
   ```bash
   git push origin feat/your-feature-name
   ```

2. Open a Pull Request against `meigma/blob:master`

3. Fill out the PR template with:
   - Summary of changes
   - Related issues (use `Fixes #123` to auto-close)
   - Testing performed

### PR Requirements

- All CI checks must pass
- Code must be formatted and lint-free
- Tests must pass (including any new tests for new functionality)
- Commits must follow Conventional Commits format
- Changes should be focused and atomic

### Review Process

- A maintainer will review your PR
- Address feedback by pushing additional commits
- Once approved, a maintainer will merge the PR

## Documentation

### Code Documentation

- Add godoc comments for all exported types, functions, and methods
- Keep comments concise and focused on "why" not "what"

### User Documentation

User-facing documentation lives in `/docs` (Docusaurus site):

```bash
cd docs
npm install
npm run start
```

Update documentation when:
- Adding new public API
- Changing library behavior
- Adding new features

## Getting Help

- **Questions**: Open a [GitHub Discussion](https://github.com/meigma/blob/discussions)
- **Bugs**: Open a [GitHub Issue](https://github.com/meigma/blob/issues)
- **Documentation**: Visit [blob.meigma.dev](https://blob.meigma.dev)

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE-APACHE) or [MIT License](LICENSE-MIT), at the user's option.
