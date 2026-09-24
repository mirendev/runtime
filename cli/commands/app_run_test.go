package commands

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

func TestRunCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"console", nil, nil},
		{"single executable", []string{"date"}, []string{"date"}},
		{"ordinary arguments", []string{"echo", "hello   world", "O'Reilly"}, []string{"echo", "hello   world", "O'Reilly"}},
		{"variable", []string{"echo", "$HOME"}, []string{"/bin/sh", "-c", "'echo' $HOME"}},
		{"pipeline with spaced argument", []string{"printf", "%s", "hello world", "|", "wc", "-c"}, []string{"/bin/sh", "-c", "'printf' '%s' 'hello world' | 'wc' '-c'"}},
		{"empty argument in pipeline", []string{"printf", "%s", "", "|", "wc", "-c"}, []string{"/bin/sh", "-c", "'printf' '%s' '' | 'wc' '-c'"}},
		{"single shell expression", []string{"echo $HOME | wc -c"}, []string{"/bin/sh", "-c", "echo $HOME | wc -c"}},
		{"single command string", []string{"exit 7"}, []string{"/bin/sh", "-c", "exit 7"}},
		{"redirect", []string{"echo", "hi", ">", "/tmp/result"}, []string{"/bin/sh", "-c", "'echo' 'hi' > '/tmp/result'"}},
		{"explicit shell", []string{"/bin/sh", "-c", "echo $HOME | wc -c"}, []string{"/bin/sh", "-c", "echo $HOME | wc -c"}},
		{"combined shell flags", []string{"sh", "-ec", "exit 7; echo unexpected"}, []string{"sh", "-ec", "exit 7; echo unexpected"}},
		{"login shell flags", []string{"bash", "-lc", "echo $1", "unused", "word"}, []string{"bash", "-lc", "echo $1", "unused", "word"}},
		{"separate shell flags", []string{"bash", "-e", "-c", "echo $1", "unused", "word"}, []string{"bash", "-e", "-c", "echo $1", "unused", "word"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runCommand(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("runCommand(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunCommandExecutesShellExpression(t *testing.T) {
	args := runCommand([]string{"printf", "%s", "hello world", "|", "wc", "-c"})
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "11" {
		t.Fatalf("pipeline output = %q, want 11", got)
	}
}

func TestRunCommandPreservesLiteralArgumentsInPipeline(t *testing.T) {
	for _, literal := range []string{"O'Reilly book", "a # b", "a = b", ""} {
		t.Run(literal, func(t *testing.T) {
			args := runCommand([]string{"printf", "%s", literal, "|", "cat"})
			out, err := exec.Command(args[0], args[1:]...).Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != literal {
				t.Fatalf("pipeline output = %q, want %q", out, literal)
			}
		})
	}
}

func TestRunCommandPreservesExplicitShellExit(t *testing.T) {
	for _, args := range [][]string{
		{"sh", "-ec", "exit 7; echo unexpected"},
		{"bash", "-lc", "exit 7; echo unexpected"},
		{"bash", "-e", "-c", "exit 7; echo unexpected"},
	} {
		got := runCommand(args)
		out, err := exec.Command(got[0], got[1:]...).CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 7 || len(out) != 0 {
			t.Fatalf("%q: exit = %v, output = %q; want code 7 and no output", args, err, out)
		}
	}

	args := runCommand([]string{"bash", "-lc", `printf '%s' "$1"`, "unused", "two words"})
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil || string(out) != "two words" {
		t.Fatalf("positional argument output = %q, error = %v; want two words", out, err)
	}
}

func TestLegacyRunCommandCheck(t *testing.T) {
	assert.NoError(t, legacyRunCommandCheck([]string{"echo", "plain text"}))
	assert.NoError(t, legacyRunCommandCheck(nil))
	assert.ErrorContains(t, legacyRunCommandCheck([]string{"echo", "$HOME"}), "require a newer cluster")
	assert.ErrorContains(t, legacyRunCommandCheck([]string{"echo", "hi", "|", "wc"}), "require a newer cluster")
}

// The compatibility fallback hinges on one distinction: a server that answered
// and does not offer app-runs (fall back to legacy exec) versus a server that
// is unreachable, wedged, or refusing us (surface the real error). Falling back
// on the wrong one would either retry an old protocol against a healthy current
// server or hide a transport failure behind a misleading legacy attempt.
func TestServerPredatesRunsOnlyForLookupFailures(t *testing.T) {
	const cap = "dev.miren.runtime/app-runs"
	const remote = "prod:8443"

	t.Run("capability absent falls back", func(t *testing.T) {
		err := rpc.NewResolveLookupError(cap, remote, "unknown object: "+cap)
		assert.True(t, serverPredatesRuns(err))
	})

	t.Run("a diagnostic-wrapped lookup error still falls back", func(t *testing.T) {
		// runsClient returns the error wrapped by RPCClient in a *ui.Diagnostic;
		// errors.Is must still reach the ResolveError through the wrap.
		lookup := rpc.NewResolveLookupError(cap, remote, "unknown object: "+cap)
		wrapped := &ui.Diagnostic{Summary: "old cluster", Cause: lookup}
		assert.True(t, serverPredatesRuns(wrapped))
	})

	notOldServer := []struct {
		name string
		err  error
	}{
		{"unreachable", rpc.NewResolveUnreachableError(cap, remote, 5*time.Second, errors.New("dial"))},
		{"went silent", rpc.NewResolveWentSilentError(cap, remote, 30*time.Second, errors.New("reset"))},
		{"no answer", rpc.NewResolveNoAnswerError(cap, remote, 8*time.Second, errors.New("timeout"))},
		{"unauthorized", rpc.NewResolveStatusError(cap, remote, 401)},
		{"unrelated error", errors.New("boom")},
	}
	for _, tc := range notOldServer {
		t.Run(tc.name+" does not fall back", func(t *testing.T) {
			assert.False(t, serverPredatesRuns(tc.err),
				"only a lookup failure means an old server; this must surface as-is")
		})
	}
}
