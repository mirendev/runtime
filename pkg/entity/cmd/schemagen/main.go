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
	fInput          = flag.String("input", "", "Input file for schema generation")
	fPkg            = flag.String("pkg", "entity", "Package name for generated code")
	fOutput         = flag.String("output", "", "output file")
	fExportContract = flag.String("export-contract", "", "optional output file for one export contract")
	fExportTarget   = flag.String("export-target", "", "export target written by -export-contract")
	fExportMerge    []string
)

func init() {
	flag.Func("export-merge", "schema file from another domain whose exports fold into this contract (repeatable)", func(path string) error {
		fExportMerge = append(fExportMerge, path)
		return nil
	})
}

func loadSchemaFile(path string) (*schemaFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sf schemaFile
	if err := yaml.NewDecoder(f).Decode(&sf); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &sf, nil
}

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

	var contributors []*schemaFile
	for _, path := range fExportMerge {
		contributor, err := loadSchemaFile(path)
		if err != nil {
			panic(err)
		}
		contributors = append(contributors, contributor)
	}

	code, err := GenerateSchema(&sf, *fPkg, contributors...)
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

	if *fExportContract != "" {
		if *fExportTarget == "" {
			panic("-export-target is required with -export-contract")
		}
		contracts, err := GenerateExportContracts(&sf, contributors...)
		if err != nil {
			panic(err)
		}
		contract, ok := contracts[*fExportTarget]
		if !ok {
			if spec, declared := sf.Exports[*fExportTarget]; declared {
				panic(fmt.Sprintf("export target %q is owned by %s; generate the contract there with -export-merge %s", *fExportTarget, spec.Owner, *fInput))
			}
			panic(fmt.Sprintf("export target %q is not declared", *fExportTarget))
		}
		if err := os.WriteFile(*fExportContract, append(contract, '\n'), 0644); err != nil {
			panic(err)
		}
	}
}
