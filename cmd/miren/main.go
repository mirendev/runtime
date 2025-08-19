package main

import (
	"os"

	"miren.dev/runtime/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
