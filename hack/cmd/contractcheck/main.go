// contractcheck compares the runtime's generated cloud export contract against
// the one cloud currently embeds, and fails when the runtime would remove or
// retype something cloud already knows. Additions always pass: cloud lands
// attributes and kinds it has not learned yet (MIR-1963).
//
// Usage: contractcheck <cloud contract> <runtime contract>
package main

import (
	"fmt"
	"io"
	"os"

	"miren.dev/runtime/pkg/entity/export"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: contractcheck <cloud contract> <runtime contract>")
		os.Exit(2)
	}
	ok, err := run(os.Stdout, os.Args[1], os.Args[2], os.Getenv("GITHUB_ACTIONS") == "true")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !ok {
		os.Exit(1)
	}
}

func run(w io.Writer, knownPath, nextPath string, annotate bool) (bool, error) {
	known, err := load(knownPath)
	if err != nil {
		return false, err
	}
	next, err := load(nextPath)
	if err != nil {
		return false, err
	}
	changes, err := export.Compare(known, next)
	if err != nil {
		return false, err
	}

	breaking := 0
	remedies := map[export.Remedy]bool{}
	for _, change := range changes {
		if change.Severity == export.Breaking {
			breaking++
			remedies[change.Remedy] = true
		}
		if annotate {
			level := "warning"
			if change.Severity == export.Breaking {
				level = "error"
			}
			fmt.Fprintf(w, "::%s file=%s,title=Cloud export contract::%s\n", level, nextPath, change)
		} else {
			fmt.Fprintf(w, "%s: %s\n", change.Severity, change)
		}
	}

	if breaking > 0 {
		fmt.Fprintf(w, "\n%d change(s) above would break cloud, which still expects the shape it embeds.\n", breaking)
		if remedies[export.ShipCloudFirst] {
			fmt.Fprint(w, `
Removals, new many-valued attributes and dropped enum values can ship
cloud-first: make just these changes to services/entitysync/cloud-export.json
in mirendev/cloud, along with any projection that reads them. Don't copy this
PR's whole contract over, since any additions in it would then look removed
from runtime main; cloud picks those up after this ships. This check reads
cloud's main branch, so it passes as soon as that merges; make sure it has
also deployed before merging this PR.
`)
		}
		if remedies[export.Replace] {
			fmt.Fprint(w, `
Type changes and moves can't be ordered safely: whichever shape cloud expects,
it rejects runtimes still sending the other, including releases already in the
field. Export the new shape as a new attribute instead, and retire the old one
once cloud no longer reads it.
`)
		}
		return false, nil
	}
	fmt.Fprintln(w, "✓ export contract is compatible with what cloud embeds")
	return true, nil
}

func load(path string) (*export.Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	contract, err := export.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return contract, nil
}
