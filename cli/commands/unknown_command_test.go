package commands

import (
	"strings"
	"testing"

	"miren.dev/mflags"
	"miren.dev/runtime/pkg/labs"
)

// dispatchErr builds the full dispatcher and returns the error Execute produces,
// so that tests can assert on what the user sees when a command is mistyped.
func dispatchErr(t *testing.T, args ...string) error {
	t.Helper()

	d := mflags.NewDispatcher("miren")
	RegisterAll(d)

	return d.Execute(args)
}

// TestUnknownCommandSuggests guards MIR-1823: a mistyped command must name the
// word that went wrong and point at the real one, rather than reporting a flag
// parsing failure.
func TestUnknownCommandSuggests(t *testing.T) {
	labs.EnableAll()

	cases := []struct {
		args    []string
		want    []string
		notWant []string
	}{
		{
			args:    []string{"depoy"},
			want:    []string{`unknown command "depoy"`, "Did you mean?", "deploy", "miren --help"},
			notWant: []string{"error parsing flags", "["},
		},
		{
			// The old message reported "error parsing flags: unexpected
			// arguments: [lst]" here.
			args:    []string{"app", "lst"},
			want:    []string{`unknown command "lst" for "miren app"`, "Did you mean?", "list", "miren app --help"},
			notWant: []string{"error parsing flags", "["},
		},
		{
			// Sections tolerate unknown flags, so this used to print help and
			// exit 0 with no sign that anything was wrong.
			args:    []string{"runner", "upgrde"},
			want:    []string{`unknown command "upgrde" for "miren runner"`, "upgrade"},
			notWant: []string{"error parsing flags"},
		},
		{
			// Nothing is close to this, so no guess should be offered.
			args:    []string{"zzzznotacommand"},
			want:    []string{`unknown command "zzzznotacommand"`},
			notWant: []string{"Did you mean?", "error parsing flags"},
		},
		{
			// "version" has no sub-commands, so there is nothing to suggest —
			// but the Go slice syntax and the flag-parsing prefix are still gone.
			args:    []string{"version", "foo"},
			want:    []string{`unexpected argument "foo"`},
			notWant: []string{"error parsing flags", "[foo]"},
		},
	}

	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			err := dispatchErr(t, c.args...)
			if err == nil {
				t.Fatalf("Execute(%v) returned no error", c.args)
			}

			got := err.Error()
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in error, got:\n%s", want, got)
				}
			}
			for _, notWant := range c.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("did not expect %q in error, got:\n%s", notWant, got)
				}
			}
		})
	}
}

// TestKnownCommandsAreNotMistakenForTypos pins the inputs the new check must
// leave alone: help keywords, pass-through arguments, and global flags that
// take a value.
func TestKnownCommandsAreNotMistakenForTypos(t *testing.T) {
	labs.EnableAll()

	cases := [][]string{
		{"app", "--help"},
		{"app", "help"},
		{"runner", "help"},
		{"help", "runner"},
		{"-C", "prod", "auth", "help"},
		{"app", "run", "echo", "hello"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if err := dispatchErr(t, args...); err != nil {
				if strings.Contains(err.Error(), "unknown command") {
					t.Errorf("Execute(%v) wrongly reported a typo: %v", args, err)
				}
			}
		})
	}
}
