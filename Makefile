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
	rm bin/runtime

bin/runtime:
	@bash ./hack/build.sh

.PHONY: bin/runtime

release:
	@bash ./hack/build-release.sh

.PHONY: release

bin/runtime-debug:
	go build -gcflags="all=-N -l" -o bin/runtime-debug ./cmd/runtime

.PHONY: bin/runtime-debug

lint-changed:
	@bash ./hack/lint-changed.sh

.PHONY: lint-changed
