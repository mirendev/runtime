#!/bin/bash

# Build script for CI environments that uses environment variables
# instead of running git commands. This is useful when building inside
# containers where git history may not be available.
#
# Required environment variables:
#   GIT_BRANCH - the branch name (e.g., "main", "release/v1.0.0")
#   GIT_COMMIT - the full commit hash
#
# Optional environment variables:
#   BUILD_DATE - build timestamp (defaults to current time)

set -euo pipefail

if [ -z "${GIT_BRANCH:-}" ]; then
  echo "Error: GIT_BRANCH environment variable is required"
  exit 1
fi

if [ -z "${GIT_COMMIT:-}" ]; then
  echo "Error: GIT_COMMIT environment variable is required"
  exit 1
fi

current_branch="$GIT_BRANCH"
commit="$GIT_COMMIT"
build_date="${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}"

# Extract short commit hash (first 7 characters)
commit_short="${commit:0:7}"

# If it's a release branch, extract the version, otherwise use branch name
if [[ $current_branch =~ ^release/(.*) ]]; then
  version="${BASH_REMATCH[1]}"
else
  version="$current_branch:$commit_short"
fi

echo "Building version $version"
echo "  Branch: $current_branch"
echo "  Commit: $commit_short"
echo "  Date:   $build_date"

go build -ldflags "\
  -X miren.dev/runtime/version.Version=$version \
  -X miren.dev/runtime/version.Commit=$commit \
  -X miren.dev/runtime/version.BuildDate=$build_date" \
  -o bin/miren ./cmd/miren
