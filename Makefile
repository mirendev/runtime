# Miren Runtime Makefile
#
# This Makefile supports two containerized build systems:
# - iso: For local development (default targets)
# - Dagger: For CI/CD (targets with -dagger suffix)
#
# Phase 1: Development uses iso, CI uses Dagger
# Phase 2: CI will migrate to iso (future)

# Silence Dagger message about using its cloud
export DAGGER_NO_NAG=1
# Disable telemetry in Dagger and anything else that honors DNT
export DO_NOT_TRACK=1

# Generate a unique session name based on the project directory
ISO_SESSION ?= dev-$(shell basename "$$(pwd)")

#
# ISO targets (for local development)
#

# Isolated test runs (clean environment every time)
test:
	iso run bash hack/test.sh ./...

test-shell:
	iso run USESHELL=1 bash hack/test.sh

test-e2e:
	iso run bash hack/test.sh ./e2e --tags=e2e

# Persistent dev environment (standalone mode)
dev-start:
	ISO_SESSION=$(ISO_SESSION) iso start && \
	ISO_SESSION=$(ISO_SESSION) iso run bash hack/dev.sh

# Start environment, server, and open shell (default for teammates)
dev: dev-start dev-server-start dev-shell

# Interactive shell
dev-shell:
	./hack/dev-exec bash

# Server lifecycle
dev-server-start:
	./hack/dev-exec bash hack/dev-server start

dev-server-stop:
	./hack/dev-exec bash hack/dev-server stop

dev-server-restart:
	./hack/dev-exec bash hack/dev-server restart

dev-server-status:
	./hack/dev-exec bash hack/dev-server status

dev-server-logs:
	./hack/dev-exec bash hack/dev-server logs

# Environment management
dev-stop:
	ISO_SESSION=$(ISO_SESSION) iso stop

dev-restart: dev-stop dev-start

dev-status:
	ISO_SESSION=$(ISO_SESSION) iso status

.PHONY: dev dev-start dev-shell dev-server-start dev-server-stop \
        dev-server-restart dev-server-status dev-server-logs \
        dev-stop dev-restart dev-status

services:
	iso run bash hack/run-services.sh

.PHONY: services

image:
	iso run sh -c "docker save runtime-shell | gzip > /workspace/tmp/miren-image.tar.gz"
	@echo "Image saved to tmp/miren-image.tar.gz"

release-data:
	iso run bash -c "make bin/miren && mkdir -p /tmp/package && \
		cp bin/miren /tmp/package && \
		cp /usr/local/bin/runc /tmp/package && \
		cp /usr/local/bin/containerd-shim-runsc-v1 /tmp/package && \
		cp /usr/local/bin/containerd-shim-runc-v2 /tmp/package && \
		cp /usr/local/bin/containerd /tmp/package && \
		cp /usr/local/bin/nerdctl /tmp/package && \
		cp /usr/local/bin/ctr /tmp/package && \
		tar -C /tmp/package -czf /workspace/release.tar.gz ."

.PHONY: release-data

#
# Dagger targets (for CI/CD)
#

test-dagger:
	dagger call -q test --dir=.

test-dagger-interactive:
	dagger call -i -q test --dir=.

test-shell-dagger:
	dagger call -q test --dir=. --shell

test-e2e-dagger:
	dagger call -q test --dir=. --tests="./e2e" --tags=e2e

dev-dagger:
	dagger call -q dev --dir=.

services-dagger:
	dagger call debug --dir=.

.PHONY: services-dagger

image-dagger:
	dagger call -q container --dir=. export --path=tmp/latest.tar
	docker import tmp/latest.tar miren:latest
	rm tmp/latest.tar

release-data-dagger:
	dagger call package --dir=. export --path=release.tar.gz

.PHONY: release-data-dagger

#
# Common targets (work without containerization)
#

clean:
	rm -f bin/miren bin/miren-debug

bin/miren:
	@bash ./hack/build.sh

.PHONY: bin/miren

release:
	@bash ./hack/build-release.sh

.PHONY: release

bin/miren-debug:
	go build -gcflags="all=-N -l" -o bin/miren-debug ./cmd/miren

.PHONY: bin/miren-debug

lint:
	golangci-lint run ./...

.PHONY: lint

lint-fix:
	golangci-lint run --fix ./...

.PHONY: lint-fix

lint-pr:
	golangci-lint run --new-from-rev main ./...

.PHONY: lint-pr

generate-check:
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

.PHONY: generate-check

dist:
	@bash ./hack/build-dist.sh

.PHONY: dist
