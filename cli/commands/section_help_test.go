package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"miren.dev/mflags"
	"miren.dev/runtime/pkg/labs"
)

// TestSectionHelpWithGlobalFlag guards against MIR-1309: a value-taking global
// flag (e.g. -C/--cluster) placed before a section name must still render the
// section's sub-commands rather than falling back to top-level help or a flag
// parse error.
func TestSectionHelpWithGlobalFlag(t *testing.T) {
	labs.EnableAll()

	cases := [][]string{
		{"auth"},
		{"auth", "help"},
		{"help", "auth"},
		{"-C", "prod", "auth"},
		{"-C", "prod", "auth", "help"},
		{"-C", "prod", "auth", "--help"},
	}

	for _, args := range cases {
		t.Run(argsName(args), func(t *testing.T) {
			out := captureDispatch(t, args)

			// The auth section's sub-commands must appear; top-level help would
			// instead list unrelated commands like "app".
			for _, want := range []string{"generate", "provider", "ci"} {
				if !bytes.Contains([]byte(out), []byte(want)) {
					t.Errorf("expected auth sub-command %q in output for %v, got:\n%s", want, args, out)
				}
			}
		})
	}
}

func argsName(args []string) string {
	var name strings.Builder
	for i, a := range args {
		if i > 0 {
			name.WriteString("_")
		}
		name.WriteString(a)
	}
	return name.String()
}

// captureDispatch builds the full dispatcher and runs Execute with the given
// args, returning everything written to stdout.
func captureDispatch(t *testing.T, args []string) string {
	t.Helper()

	d := mflags.NewDispatcher("miren")
	RegisterAll(d)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := d.Execute(args)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("Execute(%v) returned error: %v", args, err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
