#!/bin/bash
set -euo pipefail

# Create a temporary file for the Dockerfile
DOCKERFILE=$(mktemp -t dockerfile.dist.XXXXXX)

# Cleanup function
cleanup() {
    echo "Cleaning up..."
    # Remove temporary Dockerfile
    rm -f "$DOCKERFILE"
}

# Set up trap to ensure cleanup on exit, interrupt, or error
trap cleanup EXIT INT TERM

# Get version info
current_branch=$(git rev-parse --abbrev-ref HEAD)
short_sha=$(git rev-parse --short HEAD)

# Handle detached HEAD state
if [[ "$current_branch" == "HEAD" ]]; then
    # In detached HEAD state, use short SHA as version
    version="$short_sha"
else
    # Sanitize branch name: replace unsafe characters with underscores
    # Only allow alphanumeric, dots, underscores, and dashes
    sanitized_branch=$(echo "$current_branch" | sed 's/[^a-zA-Z0-9._-]/_/g')
    version="$sanitized_branch:$short_sha"
fi

echo "Building portable Linux amd64 binary (version $version)..."

# Create the Dockerfile content
cat >"$DOCKERFILE" <<EOF
FROM golang:1.25-alpine AS builder

# Accept version as a build argument
ARG VERSION

# Install build dependencies including C compiler for CGO
RUN apk add --no-cache git ca-certificates gcc musl-dev linux-headers

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Download dependencies if vendor directory doesn't exist
RUN if [ ! -d vendor ]; then go mod download; fi

# Build a binary
# We need CGO enabled for flannel dependencies
# But we'll link statically against musl libc for portability
# Version is safely passed via build arg
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X miren.dev/runtime/version.Version=\${VERSION} -linkmode external -extldflags '-static'" \
    -o miren \
    ./cmd/miren

# Use scratch for minimal final stage with just the binary
FROM scratch
COPY --from=builder /build/miren /miren
EOF

# Ensure dist directory exists
mkdir -p ./dist

# Build using Docker with version passed as build argument and export binary directly
# Specify platform to ensure consistent builds across different architectures
docker build --platform=linux/amd64 -f "$DOCKERFILE" --build-arg "VERSION=$version" --output type=local,dest=./dist .

# Rename the binary to miren-dist
mv ./dist/miren ./dist/miren-dist

# Make it executable
chmod +x ./dist/miren-dist

echo "Portable binary created at ./dist/miren-dist"

# Verify it's statically linked
echo ""
echo "Binary info:"
file ./dist/miren-dist

# Show ldd info if available (will show "not a dynamic executable" for static binaries)
if command -v ldd >/dev/null 2>&1; then
    echo ""
    echo "Dynamic dependencies:"
    ldd ./dist/miren-dist 2>&1 || echo "No dynamic dependencies (static binary)"
fi

