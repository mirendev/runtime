#!/usr/bin/env bash
set -eu

echo "Running go fmt on changed files..."
git diff --name-only HEAD | grep '\.go$' | xargs -r -n1 go fmt
git diff --cached --name-only | grep '\.go$' | xargs -r -n1 go fmt

echo "Running go vet on changed packages..."
# Get unique package directories from changed files
PACKAGES=$(
    {
        git diff --name-only HEAD | grep '\.go$' | xargs -r dirname
        git diff --cached --name-only | grep '\.go$' | xargs -r dirname
    } | sort -u | sed 's|^|./|'
)

if [ -n "$PACKAGES" ]; then
    echo "Vetting packages: $PACKAGES"
    echo $PACKAGES | xargs go vet
fi

echo "Running go mod tidy..."
go mod tidy
