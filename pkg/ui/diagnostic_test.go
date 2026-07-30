package ui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestDiagnosticErrorIsSingleLine(t *testing.T) {
	d := &Diagnostic{
		Summary: `couldn't reach cluster "local" at localhost:8443`,
		Detail:  "Nothing answered after 5s.",
		Actions: []Action{{Command: "miren doctor"}},
	}

	if got := d.Error(); got != `couldn't reach cluster "local" at localhost:8443` {
		t.Fatalf("Error() = %q", got)
	}
	if strings.Contains(d.Error(), "\n") {
		t.Fatalf("Error() must stay single-line for logs and wrapping, got %q", d.Error())
	}
}

func TestDiagnosticErrorIncludesCause(t *testing.T) {
	cause := errors.New("timeout: no recent network activity")
	d := &Diagnostic{Summary: "couldn't reach cluster", Cause: cause}

	want := "couldn't reach cluster: timeout: no recent network activity"
	if got := d.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// The underlying error has to stay reachable: doctor re-classifies failures it
// is handed, and it can only do that if errors.Is/As traverse the Diagnostic.
func TestDiagnosticUnwrapsToCause(t *testing.T) {
	sentinel := errors.New("boom")
	d := &Diagnostic{Summary: "wrapped", Cause: sentinel}

	if !errors.Is(d, sentinel) {
		t.Fatal("errors.Is could not reach the underlying cause")
	}
}

func TestDiagnosticRendersAllSections(t *testing.T) {
	d := &Diagnostic{
		Summary: `couldn't reach cluster "local" at localhost:8443`,
		Detail:  "Nothing answered after 5s. The hostname resolved, so either the server isn't running or something between here and there is blocking it.",
		Facts: []Fact{
			{Label: "Cluster", Value: "local"},
			{Label: "Address", Value: "localhost:8443"},
		},
		Causes: []string{"the server isn't running", "a firewall is blocking the connection"},
		Actions: []Action{
			{Command: "miren doctor", Note: "check what's reachable"},
			{Command: "miren cluster list", Note: "see configured clusters"},
		},
	}

	var buf bytes.Buffer
	d.WriteForTerminal(&buf)
	got := buf.String()

	for _, want := range []string{
		"ERROR:",
		`couldn't reach cluster "local" at localhost:8443`,
		"Cluster",
		"localhost:8443",
		"Possible causes",
		"the server isn't running",
		"Try",
		"miren cluster list",
		"see configured clusters",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered block missing %q\n---\n%s", want, got)
		}
	}
}

func TestDiagnosticSeverityLabel(t *testing.T) {
	d := &Diagnostic{Summary: "config is stale"}

	var buf bytes.Buffer
	d.WriteWithSeverity(&buf, SeverityWarning)

	if !strings.Contains(buf.String(), "WARNING:") {
		t.Fatalf("warning severity not reflected in output:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "ERROR:") {
		t.Fatalf("warning render leaked an ERROR label:\n%s", buf.String())
	}
}

// The raw transport error is useful under -v and noise otherwise.
func TestDiagnosticHidesCauseUnlessAsked(t *testing.T) {
	d := &Diagnostic{
		Summary: "couldn't reach cluster",
		Cause:   errors.New("timeout: no recent network activity"),
	}

	var quiet bytes.Buffer
	d.WriteForTerminal(&quiet)
	if strings.Contains(quiet.String(), "no recent network activity") {
		t.Errorf("raw cause leaked into the default render:\n%s", quiet.String())
	}

	d.ShowCause = true
	var verbose bytes.Buffer
	d.WriteForTerminal(&verbose)
	if !strings.Contains(verbose.String(), "no recent network activity") {
		t.Errorf("ShowCause did not surface the cause:\n%s", verbose.String())
	}
}

func TestDiagnosticOmitsEmptySections(t *testing.T) {
	d := &Diagnostic{Summary: "something failed"}

	var buf bytes.Buffer
	d.WriteForTerminal(&buf)

	if strings.Contains(buf.String(), "Possible causes") || strings.Contains(buf.String(), "Try") {
		t.Fatalf("empty sections rendered:\n%s", buf.String())
	}
}

// Commands must survive wrapping intact, or the copy-paste they exist for
// breaks on a narrow terminal.
func TestDiagnosticDoesNotWrapCommands(t *testing.T) {
	d := &Diagnostic{
		Summary: "failed",
		Actions: []Action{{Command: "miren server container install", Note: "run the server in Docker or Podman"}},
	}

	var buf bytes.Buffer
	d.WriteForTerminal(&buf)

	if !strings.Contains(buf.String(), "miren server container install") {
		t.Fatalf("command was broken up:\n%s", buf.String())
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  []string
	}{
		{
			name:  "wraps on word boundaries",
			input: "the quick brown fox jumps",
			width: 10,
			want:  []string{"the quick", "brown fox", "jumps"},
		},
		{
			name:  "preserves explicit line breaks",
			input: "first\nsecond",
			width: 40,
			want:  []string{"first", "second"},
		},
		{
			name:  "keeps blank lines as paragraph breaks",
			input: "first\n\nsecond",
			width: 40,
			want:  []string{"first", "", "second"},
		},
		{
			name:  "does not split a word longer than the width",
			input: "supercalifragilistic",
			width: 5,
			want:  []string{"supercalifragilistic"},
		},
		{
			name:  "collapses runs of whitespace",
			input: "a    b",
			width: 40,
			want:  []string{"a b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.input, tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("wrapText() = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("wrapText() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// ExampleDiagnostic pins the exact rendered layout. Output is plain here
// because go test's stdout isn't a terminal, which is also the pipe/CI shape.
func ExampleDiagnostic() {
	d := &Diagnostic{
		Summary: `couldn't reach cluster "prod" at cluster.example.com:8443`,
		Detail:  "Nothing answered after 5s. The hostname resolved, so either the server isn't running or something between here and there is blocking it.",
		Facts: []Fact{
			{Label: "Cluster", Value: "prod"},
			{Label: "Address", Value: "cluster.example.com:8443"},
		},
		Causes: []string{"the server isn't running", "a firewall is blocking UDP"},
		Actions: []Action{
			{Command: "miren doctor", Note: "check what's reachable"},
			{Command: "miren cluster list", Note: "see configured clusters"},
		},
	}
	d.WriteForTerminal(os.Stdout)
	// Output:
	// ERROR: couldn't reach cluster "prod" at cluster.example.com:8443
	//
	//   Nothing answered after 5s. The hostname resolved, so either the server isn't
	//   running or something between here and there is blocking it.
	//
	//   Cluster  prod
	//   Address  cluster.example.com:8443
	//
	//   Possible causes
	//     • the server isn't running
	//     • a firewall is blocking UDP
	//
	//   Try
	//     miren doctor          check what's reachable
	//     miren cluster list    see configured clusters
}
