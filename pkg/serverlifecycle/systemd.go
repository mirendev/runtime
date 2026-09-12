package serverlifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"miren.dev/runtime/pkg/release"
)

type Restarter interface {
	Restart(ctx context.Context) error
}

type SystemdRestarter struct {
	Unit string
	// StateDir is the daemon's state directory; the resource-limit drop-in
	// refreshed before each restart records stop reasons there.
	StateDir string
}

func (r SystemdRestarter) Restart(ctx context.Context) error {
	release.RefreshResourceLimits(ctx, r.Unit, r.StateDir)
	out, err := exec.CommandContext(ctx, "systemctl", "restart", r.Unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", r.Unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Launcher starts an executor somewhere that outlives the caller and the
// server.
type Launcher interface {
	Launch(ctx context.Context, opID string) error
}

// SystemdLauncher runs the executor as a transient unit, independent of
// miren.service, so restarting the server does not take it down and journald
// keeps its output. The binary is captured by inode at exec, so the executor
// keeps running the build it started with after replacing the file on disk.
type SystemdLauncher struct {
	Binary string
}

// UnitActive reports whether a unit is running or starting.
func UnitActive(ctx context.Context, unit string) bool {
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", unit).Output()
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(out)) {
	case "active", "activating":
		return true
	}
	return false
}

func UnitName(opID string) string {
	return "miren-lifecycle-" + strings.ToLower(opID)
}

func (l SystemdLauncher) Launch(ctx context.Context, opID string) error {
	args := []string{
		"--unit=" + UnitName(opID),
		"--description=miren lifecycle operation " + opID,
		"--collect",
		"--quiet",
	}
	// Transient units start from a clean environment; forward the one knob a
	// test harness needs.
	if v := os.Getenv(release.AssetBaseURLEnv); v != "" {
		args = append(args, "--setenv="+release.AssetBaseURLEnv+"="+v)
	}
	args = append(args, l.Binary, "server", "lifecycle", "run", "--operation", opID)
	out, err := exec.CommandContext(ctx, "systemd-run", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
