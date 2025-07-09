#!/usr/bin/env bash
set -eu

echo "Running linters on changed files..."
golangci-lint run --new-from-rev=HEAD --fix

echo "Running go mod tidy..."
go mod tidy
