test:
	dagger call -q test --dir=.

test-i:
	dagger call -i -q test --dir=.

test-shell:
	dagger call -q test --dir=. --shell

image:
	dagger call -q container --dir=. export --path=tmp/latest.tar
	docker import tmp/latest.tar miren:latest
	rm tmp/latest.tar

clean:
	rm bin/miren

bin/miren:
	@bash ./hack/build.sh

.PHONY: bin/miren

bin/miren-debug:
	go build -gcflags="all=-N -l" -o bin/miren-debug ./cmd/miren

.PHONY: bin/miren-debug
