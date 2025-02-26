.PHONY:
build:
	bash build.sh

.PHONY:
release: build
	tar -C release -czf os-v$$VERSION-$$PLATFORM.tar.gz .
