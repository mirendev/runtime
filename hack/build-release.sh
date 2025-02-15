#!/bin/bash

current_branch=$(git rev-parse --abbrev-ref HEAD)

# If it's a release branch, extract the version, otherwise use branch name
if [[ $current_branch =~ ^release/(.*) ]]; then
  version="${BASH_REMATCH[1]}"
else
  version="$current_branch:$(git rev-parse --short HEAD)"
fi

echo "Building version $version"

mkdir -p tmp/release

echo "Darwin / arm64"
GOOS=darwin GOARCH=arm64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o tmp/release/runtime-darwin-arm64 ./cmd/runtime

echo "Darwin / amd64"
GOOS=darwin GOARCH=amd64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o tmp/release/runtime-darwin-amd64 ./cmd/runtime

echo "Linux / arm64"
GOOS=linux GOARCH=arm64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o tmp/release/runtime-linux-arm64 ./cmd/runtime

echo "Linux / amd64"
GOOS=linux GOARCH=amd64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o tmp/release/runtime-linux-amd64 ./cmd/runtime
