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

#
# ISO targets (for local development)
#

test:
	iso run bash hack/test.sh ./...

test-shell:
	iso run USESHELL=1 bash hack/test.sh

test-e2e:
	iso run bash hack/test.sh ./e2e --tags=e2e

dev-tmux:
	iso run USE_TMUX=1 bash hack/dev.sh

dev:
	iso run bash hack/dev.sh

dev-standalone:
	iso run bash hack/dev-standalone.sh

dev-tmux-standalone:
	iso run USE_TMUX=1 bash hack/dev-standalone.sh

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

dev-tmux-dagger:
	dagger call -q dev --dir=. --tmux

dev-dagger:
	dagger call -q dev --dir=.

dev-standalone-dagger:
	dagger call -q dev-standalone --dir=.

dev-tmux-standalone-dagger:
	dagger call -q dev-tmux-standalone --dir=.

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

dist:
	@bash ./hack/build-dist.sh

.PHONY: dist
