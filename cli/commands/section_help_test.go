package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

func TestCanonicalCategoryHelp(t *testing.T) {
	for category, children := range map[string][]string{
		"deployment": {"create", "cancel"},
		"target":     {"add", "list", "remove"},
		"run":        {"attach", "list", "cancel"},
		"cloud":      {"login", "logout"},
		"route":      {"list", "delete", "set"},
		"cluster":    {"list", "switch", "upgrade"},
	} {
		t.Run(category, func(t *testing.T) {
			out := captureDispatch(t, []string{category})
			for _, child := range children {
				if !strings.Contains(out, child) {
					t.Errorf("missing %s in %s help: %s", child, category, out)
				}
			}
		})
	}
}

func TestTargetAliasKeepsJSONClean(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "shop"`)
	t.Setenv("MIREN_CONFIG", filepath.Join(t.TempDir(), "missing-config"))
	t.Setenv("MIREN_CLUSTER", "")
	for _, args := range [][]string{
		{"target", "list"},
		{"deploy", "target", "list"},
	} {
		t.Run(argsName(args), func(t *testing.T) {
			stderr, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			old := os.Stderr
			os.Stderr = stderr
			defer func() { os.Stderr = old }()
			out := captureDispatch(t, append(args, "-d", dir, "--json"))
			var targets []any
			if err := json.Unmarshal([]byte(out), &targets); err != nil || len(targets) != 0 {
				t.Fatalf("expected an empty JSON array, got %q (%v)", out, err)
			}
			stderr.Seek(0, io.SeekStart)
			warning, _ := io.ReadAll(stderr)
			if strings.Contains(string(warning), "deprecated") != (args[0] == "deploy") {
				t.Fatalf("unexpected compatibility warning: %s", warning)
			}
		})
	}
}

func TestCommandAliasSharesDeclarationNotFlagValues(t *testing.T) {
	var values []string
	example := mflags.Example{Name: "Example", Body: "miren canonical --name sample"}
	canonical := Infer("canonical", "Run the command", func(ctx *Context, opts struct {
		Name string `long:"name" default:"default"`
	}) error {
		values = append(values, opts.Name)
		if ctx.Level() != slog.LevelInfo {
			t.Errorf("alias lost daemon logging default: %v", ctx.Level())
		}
		return nil
	}, WithDaemon(), WithDescription("Shared help"), WithExample(example))
	d := mflags.NewDispatcher("miren")
	d.Dispatch("canonical", canonical)
	if err := d.Execute([]string{"canonical", "--name", "original"}); err != nil {
		t.Fatal(err)
	}

	alias := canonical.Alias("alias", WithGroup(GroupClient))
	d.Dispatch("alias", alias)
	d.Dispatch("other", canonical.Alias("other"))
	for _, args := range [][]string{{"alias", "--name", "legacy"}, {"other"}} {
		if err := d.Execute(args); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(values, []string{"original", "legacy", "default"}) {
		t.Fatalf("command and aliases leaked flag values: %v", values)
	}
	if alias.Description() != "Shared help" || len(alias.Examples()) != 1 || alias.Examples()[0] != example {
		t.Fatalf("alias lost shared help: %q, %v", alias.Description(), alias.Examples())
	}
	if alias.CommandGroup() != GroupClient || canonical.CommandGroup() != "" {
		t.Fatal("alias-specific grouping changed the canonical command")
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
