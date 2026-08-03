package commands

import (
	"context"
	"log/slog"
	"testing"

	"miren.dev/mflags"
	"miren.dev/runtime/pkg/labs"
)

// TestVerbosityLadder pins the level each -v count resolves to for both kinds of
// command. The CLI column must not move: it is the historical behaviour and
// `miren app list` staying quiet on stderr depends on it. The daemon column is
// the change — a service starts at Info so a healthy one affirms that it came
// up, instead of being indistinguishable from one that never tried.
func TestVerbosityLadder(t *testing.T) {
	cases := []struct {
		verbose int
		cli     slog.Level
		daemon  slog.Level
	}{
		{0, slog.LevelWarn, slog.LevelInfo},
		{1, slog.LevelInfo, slog.LevelDebug},
		{2, slog.LevelDebug, slog.LevelDebug},
		// Past Debug there is nowhere louder to go, so extra -v clamps rather
		// than sliding into levels no handler will ever emit.
		{3, slog.LevelDebug, slog.LevelDebug},
		{9, slog.LevelDebug, slog.LevelDebug},
	}

	for _, tc := range cases {
		flags := &GlobalFlags{Verbose: make([]bool, tc.verbose)}

		cliCtx := setup(context.Background(), flags, struct{}{}, "test", false)
		if got := cliCtx.Level(); got != tc.cli {
			t.Errorf("cli with %d -v: got %v, want %v", tc.verbose, got, tc.cli)
		}
		cliCtx.Close()

		daemonCtx := setup(context.Background(), flags, struct{}{}, "test", true)
		if got := daemonCtx.Level(); got != tc.daemon {
			t.Errorf("daemon with %d -v: got %v, want %v", tc.verbose, got, tc.daemon)
		}
		daemonCtx.Close()
	}
}

// TestWithDaemonMarksCommand checks the registration-site option actually
// reaches setup. It lives at the registration site precisely so the level is
// correct however the process was launched, so a silent failure to propagate
// would reintroduce the bug it exists to prevent.
func TestWithDaemonMarksCommand(t *testing.T) {
	noop := func(ctx *Context, opts struct{}) error { return nil }

	plain := Infer("plain", "a one-shot command", noop)
	if plain.daemon {
		t.Error("a command should not be a daemon unless it says so")
	}

	service := Infer("service", "a long-running service", noop, WithDaemon())
	if !service.daemon {
		t.Error("WithDaemon() did not mark the command")
	}
}

// TestDaemonCommandsAreRegisteredAsDaemons is the guard that matters
// operationally: the two long-running services must carry the marker. Losing it
// is silent — the process just goes quiet — and the last time a runner ran
// quieter than intended it took a fleet-wide systemd override to notice.
func TestDaemonCommandsAreRegisteredAsDaemons(t *testing.T) {
	// `runner start` only registers with distributed runners enabled, and labs
	// state is process-wide, so put it back when we're done.
	labs.Init(slog.New(slog.DiscardHandler), []string{labs.FeatureDistributedRunners})
	t.Cleanup(func() { labs.Init(slog.New(slog.DiscardHandler), nil) })

	d := mflags.NewDispatcher("miren")
	RegisterAll(d)

	for _, name := range []string{"server", "runner start"} {
		cmd, ok := d.GetCommand(name).(*Cmd)
		if !ok {
			t.Errorf("command %q is not registered as an inferred command", name)
			continue
		}
		if !cmd.daemon {
			t.Errorf("command %q must be registered WithDaemon()", name)
		}
	}
}
