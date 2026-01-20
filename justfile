# Blob archive format build tasks
set shell := ["bash", "-euo", "pipefail", "-c"]

# Output directory for generated FlatBuffers code
gen_dir := "core/internal"

# Policy module directories (separate go.mod files)
policy_modules := "policy/opa policy/sigstore policy/gittuf"

# Default recipe: validate code
default: fmt vet lint test

# CI recipe: full validation pipeline
ci: generate fmt vet lint test build

# Format check (fails if code needs formatting)
fmt:
    @echo "Checking formatting..."
    @test -z "$(gofmt -l .)" || (echo "Files need formatting:"; gofmt -l .; exit 1)

# Run go vet
vet:
    @echo "Running go vet..."
    go vet ./...
    @for mod in {{policy_modules}}; do \
        echo "Running go vet in $mod..."; \
        (cd "$mod" && go vet ./...); \
    done

# Run golangci-lint
lint:
    @echo "Running golangci-lint..."
    golangci-lint run
    @for mod in {{policy_modules}}; do \
        echo "Running golangci-lint in $mod..."; \
        (cd "$mod" && golangci-lint run); \
    done

# Run tests
test:
    @echo "Running tests..."
    go test -race -cover ./...
    @for mod in {{policy_modules}}; do \
        echo "Running tests in $mod..."; \
        (cd "$mod" && go test -race -cover ./...); \
    done

# Build the package
build:
    @echo "Building..."
    go build ./...
    @for mod in {{policy_modules}}; do \
        echo "Building $mod..."; \
        (cd "$mod" && go build ./...); \
    done

# Generate FlatBuffers code
generate:
    @echo "Generating FlatBuffers code..."
    @mkdir -p {{gen_dir}}/fb
    flatc --go --go-namespace fb -o {{gen_dir}} core/schema/index.fbs

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

# === Remote Benchmarking ===

# Setup remote bare metal benchmark server
bench-setup:
    @echo "Setting up remote benchmark environment..."
    ./scripts/remote-bench.sh setup

# Run canonical benchmarks on remote server and save results locally
bench-remote:
    ./scripts/remote-bench.sh bench-canonical

# Run arbitrary benchmarks on remote server (pass args after --)
bench-remote-raw *ARGS:
    ./scripts/remote-bench.sh bench {{ARGS}}

# Run canonical benchmark suite on remote server and store results locally
bench-remote-canonical:
    ./scripts/remote-bench.sh bench-canonical

# Show remote benchmark environment status
bench-status:
    ./scripts/remote-bench.sh status

# Teardown remote benchmark environment
bench-teardown:
    @echo "Tearing down remote benchmark environment..."
    ./scripts/remote-bench.sh teardown

# Execute arbitrary command on remote server
bench-exec *CMD:
    #!/usr/bin/env bash
    set -euo pipefail
    state_file=".bench/state.json"
    if [[ ! -f "${state_file}" ]]; then
        echo "No active setup. Run 'just bench-setup' first." >&2
        exit 1
    fi
    server_ip=$(jq -r '.server_ip' "${state_file}")
    ssh_key=$(jq -r '.ssh_key_path' "${state_file}")
    ssh -i "${ssh_key}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        "ubuntu@${server_ip}" \
        "cd /home/ubuntu/blob && export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin && {{CMD}}"
