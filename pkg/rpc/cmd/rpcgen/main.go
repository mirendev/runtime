package main

import (
	"flag"
	"os"

	"miren.dev/runtime/pkg/rpc"
)

var (
	fPkg    = flag.String("pkg", "", "package name")
	fInput  = flag.String("input", "", "input file")
	fOutput = flag.String("output", "", "output file")
)

func main() {
	flag.Parse()

	g, err := rpc.NewGenerator()
	if err != nil {
		panic(err)
	}

	err = g.Read(*fInput)
	if err != nil {
		panic(err)
	}

	output, err := g.Generate(*fPkg)
	if err != nil {
		panic(err)
	}

	f, err := os.Create(*fOutput)
	if err != nil {
		panic(err)
	}

	_, err = f.WriteString(output)
	if err != nil {
		panic(err)
	}
}
