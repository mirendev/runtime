#!/bin/bash

# Package release data by building the binary and copying all required files
# This script is designed to run inside the iso container.

set -euo pipefail

echo "Building miren binary..."
bash hack/build-ci.sh

echo "Creating package directory..."
mkdir -p /tmp/package

echo "Copying binaries to package..."
cp bin/miren /tmp/package
cp /usr/local/bin/runc /tmp/package
cp /usr/local/bin/containerd-shim-runsc-v1 /tmp/package
cp /usr/local/bin/containerd-shim-runc-v2 /tmp/package
cp /usr/local/bin/containerd /tmp/package
cp /usr/local/bin/nerdctl /tmp/package
cp /usr/local/bin/ctr /tmp/package

echo "Creating release tarball..."
tar -C /tmp/package -czf /src/release.tar.gz .

echo "Release package created at release.tar.gz"
