test:
	go test -c ./run
	cd run && sudo ../run.test -test.v
