package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/tools/imports"
	"gopkg.in/yaml.v3"
)

var (
	fInput  = flag.String("input", "", "Input file for schema generation")
	fPkg    = flag.String("pkg", "entity", "Package name for generated code")
	fOutput = flag.String("output", "", "output file")
)

func main() {
	flag.Parse()

	if *fInput == "" {
		panic("Input file must be specified")
	}

	f, err := os.Open(*fInput)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	var sf schemaFile

	if err := yaml.NewDecoder(f).Decode(&sf); err != nil {
		panic(err)
	}

	code, err := GenerateSchema(&sf, *fPkg)
	if err != nil {
		panic(err)
	}

	formatted, err := imports.Process("out.go", []byte(code), &imports.Options{})
	if err != nil {
		str := err.Error()
		lines := strings.Split(str, "\n")

		hdr := lines[0]

		var sb strings.Builder

		sb.WriteString(hdr)
		sb.WriteString("\n")

		for i, line := range lines[1:] {
			fmt.Fprintf(&sb, "%d: %s\n", i+1, line)
		}

		fmt.Println(sb.String())
		os.Exit(1)
	}

	if *fOutput == "" {
		fmt.Println(string(formatted))
	} else {
		err = os.WriteFile(*fOutput, formatted, 0644)
		if err != nil {
			panic(err)
		}
	}
}
