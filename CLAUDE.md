## Development Workflow

### Git Commits

All commits must follow the [Conventional Commits](https://www.conventionalcommits.org/) standard:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `build`, `ci`

Examples:
- `feat: add zstd compression support`
- `fix(registry): handle auth token refresh`
- `docs: update API examples`

### Pull Requests

All changes must go through pull requests. Direct commits to `master` are not allowed.

## Code Standards

### Style

All Go code must conform to the `go-style` skill. Key points:

- Standard library idioms and types (`io.Reader`, `context.Context`, etc.)
- Proper error handling with wrapped context
- Clear package organization
- Consistent naming conventions

### Testing

All Go tests must conform to the `go-testing` skill. Key points:

- Table-driven tests where appropriate
- Behavioral tests over implementation tests
- Proper use of `t.Helper()`, `t.Parallel()`
- Clear test naming

### CI Requirements

All code changes must pass CI checks before merging:

```bash
just ci
```

This runs:
1. `golangci-lint` - Static analysis and formatting
2. `go test` - All tests must pass
3. `go build` - Code must compile

<!-- BEGIN ai-protocol -->
# Agent Instructions

This repository's operating protocol lives in `.session.md`.

Before doing substantive work, read `.session.md` in full and follow it. It
covers startup context loading, session setup, session lifecycle, skill loading,
Worktrunk branching, session journaling, file schemas, architecture, and process
expectations.

If `.session.md` is missing, stop and tell the user the session protocol is not
installed correctly.
<!-- END ai-protocol -->
