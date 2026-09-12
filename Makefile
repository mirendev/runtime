# Miren Runtime Makefile
#
# This project uses iso for containerized builds and testing.

# Disable telemetry
export DO_NOT_TRACK=1

# Generate a unique session name based on the project directory.
# Exported so recipes that shell out to hack/dev-exec target the same
# session, including when ISO_SESSION is overridden from the environment.
ISO_SESSION ?= dev-$(shell basename "$$(pwd)")
export ISO_SESSION

# Extract git info on the host for passing to container builds
# These handle both regular repos and worktrees
# Use ?= so CI can override via env vars (e.g., for detached HEAD tag checkouts)
GIT_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "")
GIT_VERSION := $(shell \
  if git describe --exact-match --tags HEAD >/dev/null 2>&1; then \
    git describe --exact-match --tags HEAD; \
  elif echo "$(GIT_BRANCH)" | grep -q '^release/'; then \
    echo "$(GIT_BRANCH)" | sed 's|^release/||'; \
  elif [ -n "$(GIT_COMMIT)" ]; then \
    echo "$(GIT_BRANCH):$$(echo $(GIT_COMMIT) | cut -c1-7)"; \
  else \
    echo "$(GIT_BRANCH)"; \
  fi \
)

# Convert DEV_ENV_FILE relative paths to container paths (/src is the workdir)
# If path starts with /, use as-is (absolute)
# If path starts with ./ or is relative, prepend /src/
ifneq ($(DEV_ENV_FILE),)
  ifeq ($(shell echo "$(DEV_ENV_FILE)" | cut -c1),/)
    DEV_ENV_FILE_CONTAINER := $(DEV_ENV_FILE)
  else
    DEV_ENV_FILE_CONTAINER := /src/$(patsubst ./%,%,$(DEV_ENV_FILE))
  endif
else
  DEV_ENV_FILE_CONTAINER :=
endif

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

dev-server-start: ## Start the miren server (supports DEV_ENV_FILE and DEV_SERVER_FLAGS)
	@./hack/dev-exec --root env DEV_ENV_FILE="$(DEV_ENV_FILE_CONTAINER)" DEV_SERVER_FLAGS="$(DEV_SERVER_FLAGS)" bash hack/dev-server start
	@./hack/dev-exec --root bash hack/dev-server wait-ready

dev-server-stop: ## Stop the miren server
	@./hack/dev-exec --root bash hack/dev-server stop

dev-server-restart: ## Restart the miren server (supports DEV_ENV_FILE and DEV_SERVER_FLAGS)
	@./hack/dev-exec --root env DEV_ENV_FILE="$(DEV_ENV_FILE_CONTAINER)" DEV_SERVER_FLAGS="$(DEV_SERVER_FLAGS)" bash hack/dev-server restart
	@./hack/dev-exec --root bash hack/dev-server wait-ready

dev-server-status: ## Check miren server status
	./hack/dev-exec --root bash hack/dev-server status

dev-server-logs: ## View miren server logs
	./hack/dev-exec --root bash hack/dev-server logs

dev-start: ## Start dev environment (internal - use 'dev' instead)
	@if ! ISO_SESSION=$(ISO_SESSION) iso status 2>&1 | grep -q "Container is running"; then \
		ISO_SESSION=$(ISO_SESSION) iso start && \
		ISO_SESSION=$(ISO_SESSION) iso run GIT_BRANCH="$(GIT_BRANCH)" GIT_COMMIT="$(GIT_COMMIT)" GIT_VERSION="$(GIT_VERSION)" bash hack/dev.sh; \
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

#
# Distributed runner development (iso peers)
#

dev-distributed: ## Start distributed runner dev environment (coordinator + runner)
	@hack/dev-distributed up

dev-distributed-down: ## Stop distributed runner dev environment
	@hack/dev-distributed down

dev-distributed-status: ## Check distributed environment status
	@hack/dev-distributed status

.PHONY: dev dev-start dev-shell dev-server-start dev-server-stop \
        dev-server-restart dev-server-status dev-server-logs \
        dev-stop dev-restart dev-status dev-rebuild \
        dev-distributed dev-distributed-down dev-distributed-status

#
# Testing (iso)
#

test: ## Run all tests
	iso run bash hack/test.sh ./...

test-serial: ## Run all tests serially (no parallelism, for debugging test interference)
	iso run bash hack/test.sh -p 1 ./...

test-ci: ## Run all tests for CI
	iso run bash hack/test.sh ./...

test-shell: ## Run tests with interactive shell
	iso run USESHELL=1 bash hack/test.sh

test-coverage: ## Run tests with coverage
	iso run bash hack/test-coverage.sh ./...

test-coverage-ci: ## Run tests with coverage for CI
	iso run bash hack/test-coverage.sh ./...

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

test-groups: ## Rebuild hack/test-groups.json by measuring all test times
	python3 hack/measure-test-times.py hack/test-times.json
	python3 hack/calc-test-groups.py hack/test-times.json -n 4 -o hack/test-groups.json

update-test-groups: ## Measure new packages and rebuild hack/test-groups.json
	bash hack/update-test-times.sh hack/test-times.json
	python3 hack/calc-test-groups.py hack/test-times.json -n 4 -o hack/test-groups.json

blackbox-groups: ## Rebalance hack/blackbox-groups.json from measured blackbox test times
	python3 hack/calc-blackbox-groups.py hack/blackbox-test-times.json --env standalone -n 4 -o hack/blackbox-groups.json

measure-blackbox-times: ## Re-measure per-test blackbox times into hack/blackbox-test-times.json (requires `make dev` running)
	python3 hack/measure-blackbox-times.py -o hack/blackbox-test-times.json

blackbox-groups-distributed: ## Rebalance hack/blackbox-groups-distributed.json from measured peers test times
	python3 hack/calc-blackbox-groups.py hack/blackbox-test-times.json --env peers -n 4 -o hack/blackbox-groups-distributed.json

measure-blackbox-times-distributed: ## Re-measure per-test blackbox times in the peers topology (requires hack/dev-distributed up)
	BLACKBOX_MODE=peers python3 hack/measure-blackbox-times.py -o hack/blackbox-test-times.json

test-blackbox: ## Run blackbox tests (requires `make dev` running)
	# Cloud-backed tests are excluded: each stands up a whole cloud on fixed
	# ports and restarts the server, which interferes with everything after it.
	# Run them explicitly against a cloud checkout instead.
	go test -tags blackbox -timeout 15m -v -count=1 -p 1 -skip '^(TestPOP|TestRPCViaCloud|TestDeployViaCloud|TestServerEnrollWithToken|TestServerUnregister)$$' ./blackbox/...

build-cloud-test: ## Build cloud and POP binaries for POP blackbox tests
	@CLOUD_REPO=$${BLACKBOX_CLOUD_REPO:-$$(cd .. && pwd)/cloud}; \
	if [ ! -f "$$CLOUD_REPO/go.mod" ]; then echo "Error: Cloud repo not found at $$CLOUD_REPO. Set BLACKBOX_CLOUD_REPO."; exit 1; fi; \
	echo "Building cloud binary from $$CLOUD_REPO..."; \
	cd "$$CLOUD_REPO" && CGO_ENABLED=0 GOOS=linux go build -o $(CURDIR)/bin/bb-cloud ./cmd/cloud && \
	echo "Building POP binary from $$CLOUD_REPO..."; \
	cd "$$CLOUD_REPO" && CGO_ENABLED=0 GOOS=linux go build -o $(CURDIR)/bin/bb-pop ./cmd/pop && \
	echo "Done: bin/bb-cloud, bin/bb-pop"

test-blackbox-pop: ## Run cloud-backed blackbox tests (requires `make dev` and cloud repo)
	# Use one invocation per test so cloud environments on fixed ports and
	# server restarts cannot interfere with the next test.
	go test -tags blackbox -timeout 15m -v -count=1 -p 1 -run '^TestPOP$$' ./blackbox/...
	go test -tags blackbox -timeout 15m -v -count=1 -p 1 -run '^TestRPCViaCloud$$' ./blackbox/...
	go test -tags blackbox -timeout 15m -v -count=1 -p 1 -run '^TestDeployViaCloud$$' ./blackbox/...
	go test -tags blackbox -timeout 15m -v -count=1 -p 1 -run '^TestServerEnrollWithToken$$' ./blackbox/...
	go test -tags blackbox -timeout 15m -v -count=1 -p 1 -run '^TestServerUnregister$$' ./blackbox/...

test-blackbox-distributed: ## Run blackbox tests against distributed environment (requires hack/dev-distributed up)
	# Runs the blackbox suite (not just the distributed-specific tests) against
	# the coordinator+runner topology, so a labs flag that breaks basic
	# functionality is caught here too. TestPOP is skipped: it's the Miren
	# Anywhere cloud path (orthogonal to distributed runners) and restarts the
	# server mid-run; it has its own dedicated job.
	#
	# BLACKBOX_RUN restricts the run to a subset (a go -run alternation like
	# 'TestA|TestB'); the sharded CI matrix sets it to one shard's tests. Unset,
	# the whole suite runs, which is what local use wants. The timeout is a
	# generous upper bound for the whole-suite local case; a shard finishes well
	# inside it. Cloud-backed tests are skipped (same set as test-blackbox): an
	# unset BLACKBOX_RUN would otherwise run them here, and they need the cloud
	# repo and restart the server.
	BLACKBOX_MODE=peers go test -tags blackbox -timeout 20m -v -count=1 -p 1 \
		$(if $(BLACKBOX_RUN),-run '^($(BLACKBOX_RUN))$$') -skip '^(TestPOP|TestRPCViaCloud|TestDeployViaCloud|TestServerEnrollWithToken|TestServerUnregister)$$' ./blackbox/...

.PHONY: test test-shell test-blackbox test-blackbox-pop build-cloud-test test-blackbox-distributed test-coverage test-coverage-ci coverage-report coverage-percent coverage-by-package coverage-pr test-groups update-test-groups blackbox-groups measure-blackbox-times blackbox-groups-distributed measure-blackbox-times-distributed

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

lint: docs-lint ## Run all linters
	golangci-lint run ./...

lint-fix: ## Run golangci-lint with auto-fix
	golangci-lint run --fix ./...

lint-pr: ## Run golangci-lint on changes from main
	golangci-lint run --new-from-rev main ./...

docs-lint: ## Lint docs (no JS toolchain needed)
	@bash hack/docs-lint.sh

generate-check: bin/miren ## Verify go generate is up to date
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

.PHONY: lint lint-fix lint-pr docs-lint generate-check

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
