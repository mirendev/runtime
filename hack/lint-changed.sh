#!/usr/bin/env bash
set -euo pipefail

echo "Running go fmt on changed files..."
git diff --name-only HEAD | grep '\.go$' | xargs -r -n1 go fmt
git diff --cached --name-only | grep '\.go$' | xargs -r -n1 go fmt

echo "Running go vet on changed files..."
git diff --name-only HEAD | grep '\.go$' | xargs -r -n1 go vet
git diff --cached --name-only | grep '\.go$' | xargs -r -n1 go vet

echo "Running go mod tidy..."
go mod tidy