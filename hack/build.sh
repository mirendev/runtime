#!/bin/bash

current_branch=$(git rev-parse --abbrev-ref HEAD)

# If it's a release branch, extract the version, otherwise use branch name
if [[ $current_branch =~ ^release/(.*) ]]; then
  version="${BASH_REMATCH[1]}"
else
  version="$current_branch:$(git rev-parse --short HEAD)"
fi

echo "Building version $version"

go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o bin/miren ./cmd/miren
