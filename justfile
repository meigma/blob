# Blob archive format build tasks
set shell := ["bash", "-euo", "pipefail", "-c"]

# Output directory for generated FlatBuffers code
gen_dir := "internal/flatbuffers"

# Default recipe: validate code
default: fmt vet lint test

# CI recipe: full validation pipeline
ci: generate check-clean fmt vet lint test build

# Format check (fails if code needs formatting)
fmt:
    @echo "Checking formatting..."
    @test -z "$(gofmt -l .)" || (echo "Files need formatting:"; gofmt -l .; exit 1)

# Run go vet
vet:
    @echo "Running go vet..."
    go vet ./...

# Run golangci-lint
lint:
    @echo "Running golangci-lint..."
    golangci-lint run

# Run tests
test:
    @echo "Running tests..."
    go test -race -cover ./...

# Build the package
build:
    @echo "Building..."
    go build ./...

# Generate FlatBuffers code
generate:
    @echo "Generating FlatBuffers code..."
    @mkdir -p {{gen_dir}}
    flatc --go --go-namespace flatbuffers -o {{gen_dir}} schema/index.fbs

# Check that generated files are up to date (for CI)
check-clean:
    @echo "Checking for uncommitted changes..."
    @git diff --exit-code || (echo "Generated files are out of date. Run 'just generate' and commit."; exit 1)

# Install development tools
tools:
    @echo "Installing development tools..."
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    @echo "Note: Install flatc from https://github.com/google/flatbuffers/releases"

# Format code (modifies files)
fmt-write:
    @echo "Formatting code..."
    gofmt -w .

# Clean generated files
clean:
    @echo "Cleaning generated files..."
    rm -rf {{gen_dir}}

# Show available recipes
help:
    @just --list
