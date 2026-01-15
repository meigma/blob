#!/usr/bin/env bash
#
# remote-bench.sh - Remote bare metal benchmark environment for blob
#
# Provisions a Latitude.sh server, syncs code via Mutagen, and runs benchmarks.
#
# Usage:
#   ./scripts/remote-bench.sh setup     - Provision server and sync code
#   ./scripts/remote-bench.sh bench     - Run benchmarks on remote server
#   ./scripts/remote-bench.sh sync      - Force re-sync with Mutagen
#   ./scripts/remote-bench.sh status    - Show current state
#   ./scripts/remote-bench.sh ssh       - Open interactive SSH session
#   ./scripts/remote-bench.sh teardown  - Destroy all resources
#
set -euo pipefail

#=============================================================================
# Configuration
#=============================================================================
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly STATE_DIR="${REPO_ROOT}/.bench"
readonly STATE_FILE="${STATE_DIR}/state.json"
readonly SSH_KEY_PATH="${STATE_DIR}/id_ed25519"

# Server configuration (override via environment)
# Note: LATITUDE_PROJECT_ID should be the project SLUG (e.g., "default-project"), not the ID
: "${LATITUDE_PROJECT_ID:=}"
: "${LATITUDE_PLAN:=c2-small-x86}"
: "${LATITUDE_SITE:=LAX}"
: "${LATITUDE_OS:=ubuntu_24_04_x64_lts}"


# Naming
: "${BENCH_SERVER_NAME:=blob-bench-${USER:-anon}}"
: "${BENCH_SESSION_NAME:=blob-bench}"

# Go version (should match go.mod)
: "${GO_VERSION:=1.25.4}"

# FlatBuffers version (should match go.mod)
: "${FLATC_VERSION:=25.12.19}"

# Timeouts
: "${PROVISION_TIMEOUT:=600}"
: "${PROVISION_POLL_INTERVAL:=15}"
: "${SSH_READY_TIMEOUT:=300}"

# Remote paths
: "${REMOTE_REPO_PATH:=/home/ubuntu/blob}"

#=============================================================================
# Logging
#=============================================================================
log() { echo "[$(date '+%H:%M:%S')] $*"; }
error() { log "ERROR: $*" >&2; }
die() { error "$@"; exit 1; }

#=============================================================================
# Prerequisites
#=============================================================================
check_prerequisites() {
    local missing=()
    command -v lsh &>/dev/null || missing+=("lsh (Latitude.sh CLI)")
    command -v mutagen &>/dev/null || missing+=("mutagen")
    command -v jq &>/dev/null || missing+=("jq")
    command -v ssh-keygen &>/dev/null || missing+=("ssh-keygen")

    if [[ ${#missing[@]} -gt 0 ]]; then
        die "Missing required tools: ${missing[*]}"
    fi

    if [[ -z "${LATITUDE_PROJECT_ID}" ]]; then
        die "LATITUDE_PROJECT_ID environment variable is required"
    fi
}

#=============================================================================
# State Management
#=============================================================================
load_state() {
    if [[ ! -f "${STATE_FILE}" ]]; then
        return 1
    fi
    # Validate JSON
    jq -e . "${STATE_FILE}" &>/dev/null || return 1
    return 0
}

get_state() {
    local key="$1"
    jq -r ".${key} // empty" "${STATE_FILE}" 2>/dev/null
}

save_state() {
    local server_id="$1"
    local server_ip="$2"
    local ssh_key_id="$3"

    mkdir -p "${STATE_DIR}"
    cat > "${STATE_FILE}" <<EOF
{
  "server_id": "${server_id}",
  "server_ip": "${server_ip}",
  "ssh_key_id": "${ssh_key_id}",
  "ssh_key_path": "${SSH_KEY_PATH}",
  "mutagen_session": "${BENCH_SESSION_NAME}",
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "go_version": "${GO_VERSION}"
}
EOF
    log "State saved to ${STATE_FILE}"
}

#=============================================================================
# SSH Helpers
#=============================================================================
wait_for_ssh() {
    local host="$1"
    local key="$2"
    local elapsed=0

    log "Waiting for SSH to become available (timeout: ${SSH_READY_TIMEOUT}s)..."
    while [[ ${elapsed} -lt ${SSH_READY_TIMEOUT} ]]; do
        if ssh -i "${key}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
               -o ConnectTimeout=5 -o BatchMode=yes \
               "ubuntu@${host}" "echo ok" &>/dev/null; then
            log "SSH is ready"
            return 0
        fi
        sleep 5
        elapsed=$((elapsed + 5))
        log "  Waiting... (${elapsed}s elapsed)"
    done
    die "SSH connection timed out"
}

configure_ssh_host() {
    local host="$1"
    local key="$2"
    local config_file="${HOME}/.ssh/config"

    # Skip if config is a symlink (e.g., managed by Nix)
    if [[ -L "${config_file}" ]]; then
        log "SSH config is a symlink, skipping configuration"
        log "  Connect with: ssh -i ${key} ubuntu@${host}"
        return 0
    fi

    local marker="# blob-bench-start"
    local marker_end="# blob-bench-end"

    # Remove existing entry
    if [[ -f "${config_file}" ]]; then
        sed -i.bak "/${marker}/,/${marker_end}/d" "${config_file}"
        rm -f "${config_file}.bak"
    fi

    # Add new entry
    mkdir -p "${HOME}/.ssh"
    cat >> "${config_file}" <<EOF

${marker}
Host blob-bench
    HostName ${host}
    User ubuntu
    IdentityFile ${key}
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
${marker_end}
EOF
    log "SSH config updated (use: ssh blob-bench)"
}

remove_ssh_config() {
    local config_file="${HOME}/.ssh/config"

    # Skip if config is a symlink (e.g., managed by Nix)
    if [[ -L "${config_file}" ]]; then
        return 0
    fi

    local marker="# blob-bench-start"
    local marker_end="# blob-bench-end"

    if [[ -f "${config_file}" ]]; then
        sed -i.bak "/${marker}/,/${marker_end}/d" "${config_file}"
        rm -f "${config_file}.bak"
        log "SSH config entry removed"
    fi
}

#=============================================================================
# Mutagen Helpers
#=============================================================================
add_host_key() {
    local host="$1"
    # Add host key to known_hosts to avoid interactive prompt
    ssh-keyscan -H "${host}" >> "${HOME}/.ssh/known_hosts" 2>/dev/null
}

create_mutagen_sync() {
    local host="$1"

    log "Adding host key to known_hosts..."
    add_host_key "${host}"

    log "Creating Mutagen sync session..."
    mutagen sync create \
        --name="${BENCH_SESSION_NAME}" \
        --sync-mode="two-way-resolved" \
        --ignore=".git" \
        --ignore=".claude" \
        --ignore=".bench" \
        --ignore="*.prof" \
        --ignore="*.out" \
        --ignore="*.test" \
        --ignore="cpu.prof" \
        --ignore="mem.prof" \
        --ignore="trace.out" \
        "${REPO_ROOT}" \
        "ubuntu@${host}:${REMOTE_REPO_PATH}"

    log "Waiting for initial sync to complete..."
    mutagen sync flush "${BENCH_SESSION_NAME}"
    log "Mutagen sync session created"
}

terminate_mutagen_sync() {
    if mutagen sync list | grep -q "${BENCH_SESSION_NAME}"; then
        log "Terminating Mutagen sync session..."
        mutagen sync terminate "${BENCH_SESSION_NAME}"
    fi
}

#=============================================================================
# Remote Bootstrap
#=============================================================================
bootstrap_remote() {
    local host="$1"
    local key="$2"

    log "Bootstrapping remote environment..."

    # Create bootstrap script
    local bootstrap_script
    bootstrap_script=$(cat <<'BOOTSTRAP_EOF'
#!/bin/bash
set -euo pipefail

GO_VERSION="$1"
FLATC_VERSION="$2"

echo "=== Installing Go ${GO_VERSION} ==="
if ! command -v go &> /dev/null || [[ "$(go version 2>/dev/null)" != *"go${GO_VERSION}"* ]]; then
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
fi

echo "=== Installing FlatBuffers ${FLATC_VERSION} ==="
if ! command -v flatc &> /dev/null; then
    curl -fsSL "https://github.com/google/flatbuffers/releases/download/v${FLATC_VERSION}/Linux.flatc.binary.clang++-18.zip" -o /tmp/flatc.zip
    sudo apt-get update && sudo apt-get install -y unzip
    unzip -o /tmp/flatc.zip -d /tmp
    sudo mv /tmp/flatc /usr/local/bin/
    sudo chmod +x /usr/local/bin/flatc
    rm /tmp/flatc.zip
fi

export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin

echo "=== Installing Go tools ==="
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/perf/cmd/benchstat@latest

echo "=== Bootstrap complete ==="
go version
flatc --version
golangci-lint --version
benchstat -h 2>&1 | head -1 || true
BOOTSTRAP_EOF
)

    # Run bootstrap on remote
    # Note: arguments after -- become $1, $2 in the script
    ssh -i "${key}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        "ubuntu@${host}" "bash -s -- ${GO_VERSION} ${FLATC_VERSION}" <<< "${bootstrap_script}"

    log "Remote bootstrap complete"
}

#=============================================================================
# Commands
#=============================================================================
cmd_setup() {
    check_prerequisites

    # Check for existing state
    if load_state; then
        local existing_ip
        existing_ip=$(get_state "server_ip")
        log "Existing setup found at ${existing_ip}"
        log "Run 'teardown' first or use 'status' to check state"
        return 1
    fi

    mkdir -p "${STATE_DIR}"

    # 1. Generate SSH key
    log "Generating SSH keypair..."
    rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"
    ssh-keygen -t ed25519 -f "${SSH_KEY_PATH}" -N "" -C "${BENCH_SERVER_NAME}"

    # 2. Register key with Latitude.sh
    log "Registering SSH key with Latitude.sh..."
    local ssh_key_response
    if ! ssh_key_response=$(lsh ssh_keys create \
        --name="${BENCH_SERVER_NAME}" \
        --project="${LATITUDE_PROJECT_ID}" \
        --public_key="$(cat "${SSH_KEY_PATH}.pub")" \
        --no-input \
        --json 2>&1); then
        error "Failed to register SSH key: ${ssh_key_response}"
        rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"
        die "Setup failed"
    fi
    local ssh_key_id
    # lsh CLI returns array: [{id: "...", attributes: {...}}]
    ssh_key_id=$(echo "${ssh_key_response}" | jq -r '.[0].id // .id // empty' 2>/dev/null)
    if [[ -z "${ssh_key_id}" || "${ssh_key_id}" == "null" ]]; then
        error "Failed to parse SSH key ID from response:"
        echo "${ssh_key_response}" | head -30 >&2
        rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"
        die "Setup failed"
    fi
    log "SSH key registered: ${ssh_key_id}"

    # 3. Create server
    log "Creating bare metal server (${LATITUDE_PLAN} in ${LATITUDE_SITE})..."
    local server_response
    if ! server_response=$(lsh servers create \
        --project="${LATITUDE_PROJECT_ID}" \
        --plan="${LATITUDE_PLAN}" \
        --site="${LATITUDE_SITE}" \
        --operating_system="${LATITUDE_OS}" \
        --hostname="${BENCH_SERVER_NAME}" \
        --ssh_keys="${ssh_key_id}" \
        --billing=hourly \
        --no-input \
        --json 2>&1); then
        error "Failed to create server: ${server_response}"
        log "Cleaning up SSH key..."
        lsh ssh_keys destroy --id="${ssh_key_id}" --project="${LATITUDE_PROJECT_ID}" --no-input || true
        rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"
        die "Setup failed"
    fi
    local server_id
    # lsh CLI returns array: [{id: "...", attributes: {...}}]
    server_id=$(echo "${server_response}" | jq -r '.[0].id // .id // empty' 2>/dev/null)
    if [[ -z "${server_id}" || "${server_id}" == "null" ]]; then
        error "Failed to parse server ID from response:"
        echo "${server_response}" | head -30 >&2
        log "Cleaning up SSH key..."
        lsh ssh_keys destroy --id="${ssh_key_id}" --project="${LATITUDE_PROJECT_ID}" --no-input || true
        rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"
        die "Setup failed"
    fi
    log "Server created: ${server_id}"

    # 4. Wait for provisioning
    log "Waiting for server to provision (timeout: ${PROVISION_TIMEOUT}s)..."
    local elapsed=0
    local server_ip=""
    while [[ ${elapsed} -lt ${PROVISION_TIMEOUT} ]]; do
        local status_response
        status_response=$(lsh servers get --id="${server_id}" --json)
        local status
        # lsh CLI returns: [{attributes: {status: "...", primary_ipv4: "..."}, id: "..."}] or {attributes: ...}
        status=$(echo "${status_response}" | jq -r '.[0].attributes.status // .attributes.status // empty')

        if [[ "${status}" == "on" ]]; then
            server_ip=$(echo "${status_response}" | jq -r '.[0].attributes.primary_ipv4 // .attributes.primary_ipv4 // empty')
            break
        fi

        log "  Status: ${status} (${elapsed}s elapsed)"
        sleep "${PROVISION_POLL_INTERVAL}"
        elapsed=$((elapsed + PROVISION_POLL_INTERVAL))
    done

    if [[ -z "${server_ip}" ]]; then
        error "Server provisioning timed out"
        log "Cleaning up resources..."
        lsh servers destroy --id="${server_id}" --no-input || true
        lsh ssh_keys destroy --id="${ssh_key_id}" --project="${LATITUDE_PROJECT_ID}" --no-input || true
        rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"
        die "Setup failed"
    fi

    log "Server ready at ${server_ip}"

    # 5. Wait for SSH
    wait_for_ssh "${server_ip}" "${SSH_KEY_PATH}"

    # 6. Configure SSH
    configure_ssh_host "${server_ip}" "${SSH_KEY_PATH}"

    # 7. Create Mutagen sync
    create_mutagen_sync "${server_ip}"

    # 8. Bootstrap remote environment
    bootstrap_remote "${server_ip}" "${SSH_KEY_PATH}"

    # 9. Save state
    save_state "${server_id}" "${server_ip}" "${ssh_key_id}"

    log ""
    log "Setup complete!"
    log "  Server IP: ${server_ip}"
    log "  SSH:       ssh blob-bench"
    log "  Bench:     $0 bench"
    log "  Teardown:  $0 teardown"
}

cmd_sync() {
    if ! load_state; then
        die "No active setup. Run 'setup' first."
    fi

    log "Flushing Mutagen sync..."
    mutagen sync flush "${BENCH_SESSION_NAME}"
    log "Sync complete"
}

cmd_bench() {
    if ! load_state; then
        die "No active setup. Run 'setup' first."
    fi

    local server_ip
    server_ip=$(get_state "server_ip")

    # Sync first
    log "Syncing code..."
    mutagen sync flush "${BENCH_SESSION_NAME}"

    # Parse benchmark arguments
    local bench_args=("$@")
    if [[ ${#bench_args[@]} -eq 0 ]]; then
        bench_args=("-bench=Benchmark" "-benchmem")
    fi

    log "Running benchmarks on ${server_ip}..."
    ssh -i "${SSH_KEY_PATH}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        "ubuntu@${server_ip}" \
        "cd ${REMOTE_REPO_PATH} && export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin && go test -run='^\$' ${bench_args[*]} ./..."
}

cmd_status() {
    if ! load_state; then
        log "No active setup"
        return 0
    fi

    local server_id server_ip ssh_key_id created_at
    server_id=$(get_state "server_id")
    server_ip=$(get_state "server_ip")
    ssh_key_id=$(get_state "ssh_key_id")
    created_at=$(get_state "created_at")

    echo ""
    echo "Blob Benchmark Environment"
    echo "=========================="
    echo "Server ID:    ${server_id}"
    echo "Server IP:    ${server_ip}"
    echo "SSH Key ID:   ${ssh_key_id}"
    echo "Created:      ${created_at}"
    echo ""
    echo "Mutagen Sync:"
    mutagen sync list --label-selector="name=${BENCH_SESSION_NAME}" 2>/dev/null || \
        mutagen sync list 2>/dev/null | grep -A5 "${BENCH_SESSION_NAME}" || \
        echo "  Session not found"
    echo ""
}

cmd_ssh() {
    if ! load_state; then
        die "No active setup. Run 'setup' first."
    fi

    local server_ip
    server_ip=$(get_state "server_ip")

    log "Connecting to ${server_ip}..."
    exec ssh -i "${SSH_KEY_PATH}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        "ubuntu@${server_ip}"
}

cmd_teardown() {
    if ! load_state; then
        log "No active setup to teardown"
        return 0
    fi

    local server_id ssh_key_id
    server_id=$(get_state "server_id")
    ssh_key_id=$(get_state "ssh_key_id")

    # 1. Terminate Mutagen
    terminate_mutagen_sync

    # 2. Destroy server
    if [[ -n "${server_id}" ]]; then
        log "Destroying server ${server_id}..."
        lsh servers destroy --id="${server_id}" --no-input || true
    fi

    # 3. Delete SSH key from Latitude.sh
    if [[ -n "${ssh_key_id}" ]]; then
        log "Deleting SSH key ${ssh_key_id}..."
        lsh ssh_keys destroy --id="${ssh_key_id}" --project="${LATITUDE_PROJECT_ID}" --no-input || true
    fi

    # 4. Remove local SSH key
    rm -f "${SSH_KEY_PATH}" "${SSH_KEY_PATH}.pub"

    # 5. Remove SSH config
    remove_ssh_config

    # 6. Remove state
    rm -f "${STATE_FILE}"
    rmdir "${STATE_DIR}" 2>/dev/null || true

    log "Teardown complete"
}

cmd_help() {
    cat <<EOF
Usage: $0 <command> [options]

Commands:
  setup      Provision server, sync code, and bootstrap environment
  bench      Run benchmarks on remote server (passes args to go test)
  sync       Force re-sync code with Mutagen
  status     Show current environment state
  ssh        Open interactive SSH session to server
  teardown   Destroy server and clean up all resources

Environment Variables:
  LATITUDE_PROJECT_ID  (required) Latitude.sh project slug (e.g., "default-project")
  LATITUDE_PLAN        Server plan (default: c2-small-x86)
  LATITUDE_SITE        Datacenter site (default: LAX)
  LATITUDE_OS          Operating system (default: ubuntu_24_04_x64_lts)
  GO_VERSION           Go version to install (default: 1.25.4)

Examples:
  # Initial setup
  export LATITUDE_PROJECT_ID="your-project-id"
  $0 setup

  # Run all benchmarks
  $0 bench

  # Run specific benchmark
  $0 bench -bench=BenchmarkReaderCopyDir -count=10

  # Clean up
  $0 teardown
EOF
}

#=============================================================================
# Main
#=============================================================================
main() {
    local cmd="${1:-help}"
    shift || true

    case "${cmd}" in
        setup)    cmd_setup "$@" ;;
        sync)     cmd_sync "$@" ;;
        bench)    cmd_bench "$@" ;;
        status)   cmd_status "$@" ;;
        ssh)      cmd_ssh "$@" ;;
        teardown) cmd_teardown "$@" ;;
        help|--help|-h) cmd_help ;;
        *)        die "Unknown command: ${cmd}. Run '$0 help' for usage." ;;
    esac
}

main "$@"
