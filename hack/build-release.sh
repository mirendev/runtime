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

dir="tmp/release/$version"

mkdir -p $dir

if ! test -f $dir/miren-darwin-arm64.zip; then
  echo "Darwin / arm64"
  GOOS=darwin GOARCH=arm64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  zip -j $dir/miren-darwin-arm64.zip $dir/miren

  rm $dir/miren
fi

if ! test -f $dir/miren-darwin-amd64.zip; then
  echo "Darwin / amd64"
  GOOS=darwin GOARCH=amd64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  zip -j $dir/miren-darwin-amd64.zip $dir/miren

  rm $dir/miren
fi

if ! test -f $dir/miren-linux-arm64.zip; then
  echo "Linux / arm64"
  GOOS=linux GOARCH=arm64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  zip -j $dir/miren-linux-arm64.zip $dir/miren

  rm $dir/miren
fi

if ! test -f $dir/miren-linux-amd64.zip; then
  echo "Linux / amd64"
  GOOS=linux GOARCH=amd64 go build -ldflags "-X miren.dev/runtime/version.Version=$version" -o $dir/miren ./cmd/miren

  zip -j $dir/miren-linux-amd64.zip $dir/miren

  rm $dir/miren
fi
