#!/bin/bash

# Script to generate version.json metadata file for release

set -e

# Get git information
COMMIT=$(git rev-parse HEAD)
COMMIT_SHORT=$(git rev-parse --short HEAD)
BRANCH=$(git rev-parse --abbrev-ref HEAD)
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Determine version string (same logic as build.sh)
if [[ $BRANCH =~ ^release/(.*) ]]; then
  VERSION="${BASH_REMATCH[1]}"
else
  VERSION="$BRANCH:$COMMIT_SHORT"
fi

# List of artifacts that will be available for this release
# This should match what's actually uploaded in the GitHub workflow
ARTIFACTS='[
  "miren-base-linux-amd64.tar.gz",
  "miren-base-linux-arm64.tar.gz",
  "miren-linux-amd64.zip",
  "miren-linux-arm64.zip",
  "miren-darwin-amd64.zip",
  "miren-darwin-arm64.zip",
  "docker-compose.yml"
]'

# Generate JSON metadata
cat > version.json <<EOF
{
  "version": "$VERSION",
  "commit": "$COMMIT",
  "branch": "$BRANCH",
  "build_date": "$BUILD_DATE",
  "artifacts": $ARTIFACTS
}
EOF

echo "Generated version.json:"
cat version.json