#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

# Ensure dev container is running (without attaching to shell)
./hack/dev-start.sh

# Execute command in dev container
docker exec miren-dev bash -c "cd /src && $*"
