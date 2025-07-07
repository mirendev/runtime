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
setup_runsc_config

# Start services
start_containerd "$CONTAINERD_ADDRESS" "/dev/null"
start_buildkitd "/dev/stdout"

# Setup kernel mounts
setup_kernel_mounts

# Wait for services
wait_for_service "containerd" "ctr --address '$CONTAINERD_ADDRESS' version"
wait_for_service "buildkitd" "buildctl debug info"

cd /src

if test "$USESHELL" != ""; then
  setup_bash_environment
  bash
# Because all the tests use the same containerd, buildkit, and cgroups, we need to
# make sure that they don't interfere with each other. For now, we do that by passing
# -p 1, but in the future we should run each test in a separate namespace.
elif test "$VERBOSE" != ""; then
  go test -p 1 -v "$@"
else
  gotestsum --format testname -- -p 1 "$@"
fi
