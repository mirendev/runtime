test:
	dagger call -q test --dir=.

test-i:
	dagger call -i -q test --dir=.

test-shell:
	dagger call -q test --dir=. --shell

dev:
	dagger call -q dev --dir=. --shell

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
