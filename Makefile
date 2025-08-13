# Silence Dagger message about using its cloud
export DAGGER_NO_NAG=1
# Disable telemetry in Dagger and anything else that honors DNT
export DO_NOT_TRACK=1

test:
	dagger call -q test --dir=.

test-i:
	dagger call -i -q test --dir=.

test-shell:
	dagger call -q test --dir=. --shell

test-e2e:
	dagger call -q test --dir=. --tests="./e2e" --tags=e2e

dev-tmux:
	dagger call -q dev --dir=. --tmux

dev:
	dagger call -q dev --dir=.

services:
	dagger call debug --dir=.

.PHONY: services

image:
	dagger call -q container --dir=. export --path=tmp/latest.tar
	docker import tmp/latest.tar miren:latest
	rm tmp/latest.tar

release-data:
	dagger call package --dir=. export --path=release.tar.gz

.PHONY: release-data

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

lint-changed:
	@bash ./hack/lint-changed.sh

.PHONY: lint-changed

dist:
	@bash ./hack/build-dist.sh

.PHONY: dist
