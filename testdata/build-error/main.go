package main

// This file intentionally does not compile: the blackbox suite uses it to
// exercise a build that fails during the buildkit stage, so the server settles
// the deployment record as failed with the compiler error as its logs.
func main() {
	this is not valid go
}
