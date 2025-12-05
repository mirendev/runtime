#!/usr/bin/env bash
set -e

# Source common setup functions
source "$(dirname "$0")/common-setup.sh"

# Setup environment
setup_cgroups
setup_environment

export CONTAINERD_ADDRESS="/run/containerd/containerd.sock"

# Generate configs
generate_containerd_config

# Start services
start_containerd "$CONTAINERD_ADDRESS" "/dev/null"
start_buildkitd "/dev/stdout"

# Setup kernel mounts
setup_kernel_mounts

# Wait for services
wait_for_service "containerd" "ctr --address '$CONTAINERD_ADDRESS' version"
wait_for_service "buildkitd" "buildctl debug info"

cd /src

# Normalize package path arguments for convenience
# Supports: pkg/entity, ./pkg/entity, miren.dev/runtime/pkg/entity
normalize_args() {
  local args=()
  for arg in "$@"; do
    # Check if this looks like a package path (not a flag starting with -)
    if [[ ! "$arg" =~ ^- ]] && [[ "$arg" =~ / ]]; then
      # If it starts with the module path, convert to relative
      if [[ "$arg" =~ ^miren\.dev/runtime/ ]]; then
        arg="./${arg#miren.dev/runtime/}"
      # If it doesn't start with ./ and is an actual directory path (or pattern), add ./
      elif [[ ! "$arg" =~ ^\. ]]; then
        # Only add ./ if it's a real directory or a go package pattern
        if [[ -d "$arg" ]] || [[ "$arg" =~ \.\.\. ]]; then
          arg="./$arg"
        fi
      fi
    fi
    args+=("$arg")
  done
  echo "${args[@]}"
}

# Default to ./... if no packages specified
PACKAGES="${1:-./...}"
normalized_args=($(normalize_args "$PACKAGES"))

# Run tests with coverage
# -p 1: no parallelism (required for shared containerd/buildkit)
# -coverprofile: output coverage data
# -covermode=atomic: precise coverage with race condition handling
echo "Running tests with coverage for ${normalized_args[@]}..."
gotestsum --format testname -- -p 1 -coverprofile=coverage.out -covermode=atomic "${normalized_args[@]}"

# Calculate coverage percentage
if [ -f coverage.out ]; then
  # Filter out generated files from coverage
  grep -v '.gen.go' coverage.out > coverage.filtered.out || true
  mv coverage.filtered.out coverage.out

  # Calculate total coverage
  COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
  echo ""
  echo "======================================"
  echo "Total Coverage: ${COVERAGE}%"
  echo "======================================"
  echo ""

  # Export for use in CI
  echo "COVERAGE=${COVERAGE}" >> "${GITHUB_OUTPUT:-/dev/null}" 2>/dev/null || true
else
  echo "Warning: coverage.out not found"
  exit 1
fi
