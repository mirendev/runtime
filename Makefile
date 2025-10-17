test:
	@./hack/dev-run.sh go test -p 1 ./...

test-e2e:
	@./hack/dev-run.sh go test -p 1 -tags=e2e ./e2e

dev:
	@./hack/dev-start.sh shell

dev-stop:
	@docker stop miren-dev 2>/dev/null || true
	@docker rm miren-dev 2>/dev/null || true
	@docker compose down

.PHONY: test test-e2e dev dev-stop

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
