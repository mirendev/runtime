#!/usr/bin/env bash
set -e

# Source common setup functions
source "$(dirname "$0")/common-setup.sh"

# Setup environment
setup_cgroups
setup_environment

export CONTAINERD_ADDRESS="/run/containerd/containerd.sock"
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"

# Generate configs
generate_containerd_config

# Start services
start_containerd "$CONTAINERD_ADDRESS" "/dev/null"
start_buildkitd "/dev/null"

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
      # x/ is its own module, which a ./ path from the root cannot reach,
      # so its packages keep their import path.
      if [[ "$arg" =~ ^miren\.dev/runtime/x(/|$) ]]; then
        :
      # If it starts with the module path, convert to relative
      elif [[ "$arg" =~ ^miren\.dev/runtime/ ]]; then
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

if test "$USESHELL" != ""; then
  setup_bash_environment
  bash
# Tests use unique containerd namespaces and dynamic ports, so they should be
# safe to run in parallel. Remove -p 1 to enable parallel package execution.
elif test "$VERBOSE" != ""; then
  normalized_args=($(normalize_args "$@"))
  go test -v "${normalized_args[@]}"
else
  normalized_args=($(normalize_args "$@"))
  if [ -n "${TESTFMT_JSON:-}" ]; then
    go test -json "${normalized_args[@]}"
  elif [ -n "${TEST_RERUN_FAILS:-}" ]; then
    # Rerun failed tests up to TEST_RERUN_FAILS times, for suites that depend on
    # upstream registries. gotestsum takes the packages apart from the go test
    # flags here, so flags must use the -flag=value form. Reruns are listed in
    # TEST_RERUN_REPORT when it's set.
    pkgs=()
    flags=()
    for arg in "${normalized_args[@]}"; do
      if [[ "$arg" =~ ^- ]]; then
        flags+=("$arg")
      else
        pkgs+=("$arg")
      fi
    done
    gotestsum --format testname \
      --rerun-fails="$TEST_RERUN_FAILS" \
      ${TEST_RERUN_REPORT:+--rerun-fails-report="$TEST_RERUN_REPORT"} \
      --packages="${pkgs[*]}" -- "${flags[@]}"
  else
    gotestsum --format testname -- "${normalized_args[@]}"
  fi
fi
