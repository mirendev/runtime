#!/bin/bash

# Use env vars if set (for CI/container builds), otherwise ask the VCS.
# vcs-info.sh degrades gracefully when neither git nor jj can see a repo,
# which is the case inside the iso container.
build_date=${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}

source "$(dirname "$0")/vcs-info.sh"
current_branch="$GIT_BRANCH"
commit="$GIT_COMMIT"

# Determine version string
if [ -n "${GIT_VERSION:-}" ]; then
  # Explicit version override
  version="$GIT_VERSION"
elif [ -n "$GIT_TAG" ]; then
  # Current commit has a tag
  version="$GIT_TAG"
elif [[ $current_branch =~ ^release/(.*) ]]; then
  # On release branch
  version="${BASH_REMATCH[1]}"
elif [ -n "$commit" ]; then
  # Fall back to branch:short-commit
  version="$current_branch:${commit:0:7}"
else
  # No git info available
  version="$current_branch"
fi

echo "Building version $version"
echo "  Commit: ${commit:0:7}"
echo "  Date:   $build_date"

go build -ldflags "\
  -X miren.dev/runtime/version.Version=$version \
  -X miren.dev/runtime/version.Commit=$commit \
  -X miren.dev/runtime/version.BuildDate=$build_date" \
  -o bin/miren ./cmd/miren
