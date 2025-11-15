# Miren Runtime Makefile
#
# This project uses iso for containerized builds and testing.

# Disable telemetry
export DO_NOT_TRACK=1

# Generate a unique session name based on the project directory
ISO_SESSION ?= dev-$(shell basename "$$(pwd)")

.PHONY: help
help: ## Show this help message
	@echo "Miren Runtime"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_/-]+:.*?## / {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

#
# Development (iso - recommended)
#

dev: dev-start dev-server-start dev-shell ## Start dev environment, server, and open shell

dev-shell: ## Open interactive shell in dev environment
	@./hack/dev-exec bash

dev-server-start: ## Start the miren server
	./hack/dev-exec bash hack/dev-server start

dev-server-stop: ## Stop the miren server
	./hack/dev-exec bash hack/dev-server stop

dev-server-restart: ## Restart the miren server
	./hack/dev-exec bash hack/dev-server restart

dev-server-status: ## Check miren server status
	./hack/dev-exec bash hack/dev-server status

dev-server-logs: ## View miren server logs
	./hack/dev-exec bash hack/dev-server logs

dev-start: ## Start dev environment (internal - use 'dev' instead)
	@if ! ISO_SESSION=$(ISO_SESSION) iso status 2>&1 | grep -q "Container is running"; then \
		ISO_SESSION=$(ISO_SESSION) iso start && \
		ISO_SESSION=$(ISO_SESSION) iso run bash hack/dev.sh; \
	else \
		echo "✓ Container already running"; \
	fi

dev-stop: ## Stop the dev environment
	ISO_SESSION=$(ISO_SESSION) iso stop

dev-restart: dev-stop dev-start ## Restart the dev environment

dev-status: ## Check dev environment status
	ISO_SESSION=$(ISO_SESSION) iso status

dev-rebuild: ## Rebuild dev environment image
	ISO_SESSION=$(ISO_SESSION) iso build --rebuild

.PHONY: dev dev-start dev-shell dev-server-start dev-server-stop \
        dev-server-restart dev-server-status dev-server-logs \
        dev-stop dev-restart dev-status dev-rebuild \
        dev-stop dev-restart dev-status

#
# Testing (iso)
#

test: ## Run all tests
	iso run bash hack/test.sh ./...

test-ci: ## Run all tests for CI
	iso run DISABLE_NBD_TEST=1 bash hack/test.sh ./...

test-shell: ## Run tests with interactive shell
	iso run USESHELL=1 bash hack/test.sh

test-e2e: ## Run end-to-end tests
	iso run bash hack/test.sh ./e2e --tags=e2e

test-coverage: ## Run tests with coverage
	iso run bash hack/test-coverage.sh ./...

test-coverage-ci: ## Run tests with coverage for CI
	iso run DISABLE_NBD_TEST=1 bash hack/test-coverage.sh ./...

coverage-report: ## Generate HTML coverage report
	@if [ ! -f coverage.out ]; then \
		echo "Error: coverage.out not found. Run 'make test-coverage' first."; \
		exit 1; \
	fi
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

coverage-percent: ## Display coverage percentage
	@if [ ! -f coverage.out ]; then \
		echo "Error: coverage.out not found. Run 'make test-coverage' first."; \
		exit 1; \
	fi
	@go tool cover -func=coverage.out | grep total | awk '{print "Total Coverage: " $$3}'

coverage-by-package: ## Display coverage percentage by package
	@if [ ! -f coverage.out ]; then \
		echo "Error: coverage.out not found. Run 'make test-coverage' first."; \
		exit 1; \
	fi
	@go run ./hack/coverage-by-package -coverage=coverage.out $(ARGS)

coverage-pr: ## Display coverage for lines changed in current branch
	@if [ ! -f coverage.out ]; then \
		echo "Error: coverage.out not found. Run 'make test-coverage' first."; \
		exit 1; \
	fi
	@go run ./hack/coverage-changed-lines -coverage=coverage.out $(ARGS)

.PHONY: test test-shell test-e2e test-coverage test-coverage-ci coverage-report coverage-percent coverage-by-package coverage-pr

#
# Building
#

bin/miren: ## Build the miren binary
	@bash ./hack/build.sh

release: ## Build release version
	@bash ./hack/build-release.sh

bin/miren-debug: ## Build with debug symbols
	go build -gcflags="all=-N -l" -o bin/miren-debug ./cmd/miren

clean: ## Remove built binaries
	rm -f bin/miren bin/miren-debug

.PHONY: bin/miren release bin/miren-debug clean

#
# Code Quality
#

lint: ## Run golangci-lint
	golangci-lint run ./...

lint-fix: ## Run golangci-lint with auto-fix
	golangci-lint run --fix ./...

lint-pr: ## Run golangci-lint on changes from main
	golangci-lint run --new-from-rev main ./...

generate-check: ## Verify go generate is up to date
	@echo "Checking if go generate produces any changes..."
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory is not clean. Please commit or stash changes before running generate-check."; \
		exit 1; \
	fi
	@go generate ./...
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo ""; \
		echo "Error: go generate produced changes. Please run 'go generate ./...' and commit the results."; \
		echo ""; \
		echo "Files that changed:"; \
		git status --short; \
		git diff; \
		exit 1; \
	fi
	@echo "✓ go generate is up to date"

.PHONY: lint lint-fix lint-pr generate-check

#
# Release Packaging
#

release-data: ## Create release package tar.gz (iso)
	bash hack/release-data.sh

image: ## Export Docker image (iso)
	iso run sh -c "docker save runtime-shell | gzip > /workspace/tmp/miren-image.tar.gz"
	@echo "Image saved to tmp/miren-image.tar.gz"

dist: ## Build distribution packages
	@bash ./hack/build-dist.sh

.PHONY: release-data image dist

#
# Other (iso)
#

services: ## Run services container for debugging
	iso run bash hack/run-services.sh

.PHONY: services
