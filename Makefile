test:
	dagger call -q test --dir=.

test-i:
	dagger call -i -q test --dir=.

test-shell:
	dagger call -q test --dir=. --shell

clean:
	rm bin/miren

bin/miren:
	@bash ./hack/build.sh
