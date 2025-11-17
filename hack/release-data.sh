#!/bin/bash

# Create release data package by building and packaging miren with all dependencies.
# This script handles extracting git information from the host environment
# before running the build inside the iso container.
#
# Environment variables:
#   GIT_BRANCH - branch name (optional, will be extracted from git if not set)
#   GIT_COMMIT - commit hash (optional, will be extracted from git if not set)
#   BUILD_DATE - build timestamp (optional, defaults to current time)

set -euo pipefail

# Use env vars if set (for CI), otherwise extract from git (for local dev)
# Check if current commit has a tag first
if [ -z "${GIT_BRANCH:-}" ] && git describe --exact-match --tags HEAD 2>/dev/null; then
  GIT_BRANCH=$(git describe --exact-match --tags HEAD)
else
  GIT_BRANCH=${GIT_BRANCH:-$(git rev-parse --abbrev-ref HEAD)}
fi
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse HEAD)}
BUILD_DATE=${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}

echo "Creating release package..."
echo "  Branch: $GIT_BRANCH"
echo "  Commit: ${GIT_COMMIT:0:7}"
echo "  Date:   $BUILD_DATE"

# Run the package script inside iso with git info from host
iso run \
  GIT_BRANCH="$GIT_BRANCH" \
  GIT_COMMIT="$GIT_COMMIT" \
  BUILD_DATE="$BUILD_DATE" \
  bash hack/package-release.sh
