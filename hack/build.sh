#!/bin/bash

current_branch=$(git rev-parse --abbrev-ref HEAD)
commit=$(git rev-parse HEAD)
build_date=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Check if current commit has a tag
if git describe --exact-match --tags HEAD 2>/dev/null; then
  version=$(git describe --exact-match --tags HEAD)
# Check if on release branch
elif [[ $current_branch =~ ^release/(.*) ]]; then
  version="${BASH_REMATCH[1]}"
# Fall back to branch:commit
else
  version="$current_branch:$(git rev-parse --short HEAD)"
fi

echo "Building version $version"
echo "  Commit: ${commit:0:7}"
echo "  Date:   $build_date"

go build -ldflags "\
  -X miren.dev/runtime/version.Version=$version \
  -X miren.dev/runtime/version.Commit=$commit \
  -X miren.dev/runtime/version.BuildDate=$build_date" \
  -o bin/miren ./cmd/miren
