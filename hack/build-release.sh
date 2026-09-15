#!/bin/bash

if test -z "$VERSION"; then

  current_branch=$(git rev-parse --abbrev-ref HEAD)

  # If it's a release branch, extract the version, otherwise use branch name
  if [[ $current_branch =~ ^release/(.*) ]]; then
    version="${BASH_REMATCH[1]}"
  else
    version="$current_branch:$(git rev-parse --short HEAD)"
  fi
else
  version="$VERSION"
fi

echo "Building version $version"

# Static builds, matching hack/build.sh and hack/build-ci.sh.
export CGO_ENABLED=0

dir="tmp/release/$version"

mkdir -p $dir

if ! test -f $dir/miren-darwin-arm64.tar.gz || ! test -f $dir/miren-darwin-arm64.zip; then
  echo "Darwin / arm64"
  GOOS=darwin GOARCH=arm64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  tar -czf $dir/miren-darwin-arm64.tar.gz -C $dir miren
  # Transitional: also produce .zip for older clients
  zip -j $dir/miren-darwin-arm64.zip $dir/miren

  rm $dir/miren
fi

if ! test -f $dir/miren-darwin-amd64.tar.gz || ! test -f $dir/miren-darwin-amd64.zip; then
  echo "Darwin / amd64"
  GOOS=darwin GOARCH=amd64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  tar -czf $dir/miren-darwin-amd64.tar.gz -C $dir miren
  # Transitional: also produce .zip for older clients
  zip -j $dir/miren-darwin-amd64.zip $dir/miren

  rm $dir/miren
fi

if ! test -f $dir/miren-linux-arm64.tar.gz || ! test -f $dir/miren-linux-arm64.zip; then
  echo "Linux / arm64"
  GOOS=linux GOARCH=arm64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  tar -czf $dir/miren-linux-arm64.tar.gz -C $dir miren
  # Transitional: also produce .zip for older clients
  zip -j $dir/miren-linux-arm64.zip $dir/miren

  rm $dir/miren
fi

if ! test -f $dir/miren-linux-amd64.tar.gz || ! test -f $dir/miren-linux-amd64.zip; then
  echo "Linux / amd64"
  GOOS=linux GOARCH=amd64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  tar -czf $dir/miren-linux-amd64.tar.gz -C $dir miren
  # Transitional: also produce .zip for older clients
  zip -j $dir/miren-linux-amd64.zip $dir/miren

  rm $dir/miren
fi
