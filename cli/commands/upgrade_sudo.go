package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/ui"
)

// sudoForwardedEnv lists the variables that change what an upgrade does, so
// they have to survive sudo's env_reset. It is an allowlist on purpose: a
// variable like MIREN_CONFIG would quietly point the root process at the
// invoking user's client config.
var sudoForwardedEnv = []string{release.AssetBaseURLEnv}

// sudoRerun is a re-run of this exact invocation under sudo, along with the
// plain-language list of what it will do with root.
type sudoRerun struct {
	argv  []string
	steps []string
}

// newSudoRerun returns false when sudo is not installed, in which case there
// is nothing to offer.
func newSudoRerun(exe string, steps []string) (*sudoRerun, bool) {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return nil, false
	}
	return &sudoRerun{
		argv:  sudoArgv(sudo, exe, os.Args[1:], os.Getenv),
		steps: steps,
	}, true
}

// sudoArgv builds the command line for the re-run. The forwarded variables
// go through env(1) on the root side rather than as `sudo VAR=value`, which
// a sudoers policy without SETENV refuses.
func sudoArgv(sudo, exe string, args []string, getenv func(string) string) []string {
	argv := []string{sudo}
	var env []string
	for _, k := range sudoForwardedEnv {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	if len(env) > 0 {
		argv = append(argv, "env")
		argv = append(argv, env...)
	}
	argv = append(argv, exe)
	return append(argv, args...)
}

// explain says exactly what root will be used for before anyone is asked
// for a password.
func (r *sudoRerun) explain(ctx *Context) {
	ctx.Info("With sudo, this will:")
	for _, step := range r.steps {
		ctx.Info("  - %s", step)
	}
	ctx.Info("")
	ctx.Info("Command: %s", shellJoin(r.argv))
	ctx.Info("")
}

// exec replaces this process with the sudo re-run, so the password prompt,
// signals, and exit status all belong to the re-run. It returns only on
// failure.
func (r *sudoRerun) exec(ctx *Context) error {
	ctx.Info("Re-running with sudo...")
	if err := syscall.Exec(r.argv[0], r.argv, os.Environ()); err != nil {
		return fmt.Errorf("failed to re-run with sudo: %w", err)
	}
	return nil
}

// confirmDaemonSudo asks whether to re-run a daemon upgrade under sudo.
func confirmDaemonSudo(ctx *Context, daemon lifecycleDaemon, rerun *sudoRerun) (bool, error) {
	ctx.Warn("This host runs the miren %s, and upgrading it needs root.", daemon.name)
	ctx.Info("")
	rerun.explain(ctx)

	items := []ui.PickerItem{
		ui.SimplePickerItem{Text: "Re-run with sudo"},
		ui.SimplePickerItem{Text: "Cancel"},
	}
	selected, err := ui.RunPicker(items, ui.WithTitle("How would you like to proceed?"))
	if err != nil {
		return false, fmt.Errorf("failed to run picker: %w", err)
	}
	return selected != nil && selected.ID() == "Re-run with sudo", nil
}

// daemonSudoSteps describes a root upgrade of daemon, driven from the CLI
// binary at exe.
func daemonSudoSteps(daemon lifecycleDaemon, exe string) []string {
	mgr := daemon.manager()
	steps := []string{"download the target miren release, unless this host already runs it"}
	if daemon.name == serverDaemon.name {
		// Whether etcd is embedded lives in root-only config and the unit's
		// environment, so the step states the condition rather than guess.
		steps = append(steps, "snapshot etcd first when it is embedded (the default), so a failed upgrade can roll back the data too; with external etcd there is no snapshot, and rollback restores only the binary")
	}
	steps = append(steps,
		fmt.Sprintf("install the new binary at %s and point %s at it", mgr.InstallPath, mgr.PathSymlink),
		fmt.Sprintf("restart the %s systemd service, rolling back if it does not come back healthy", mgr.ServiceName),
	)
	if daemon.name == serverDaemon.name {
		steps = append(steps, "ask this cluster's runners, if any, to upgrade to the same build")
	}
	if !sharesBinary(mgr.InstallPath, exe) {
		steps = append(steps, fmt.Sprintf("copy the new build over this CLI at %s (the previous one is kept as %s.old)", exe, exe))
	}
	return steps
}

// cliSudoSteps describes a root upgrade of the CLI binary alone.
func cliSudoSteps(exe string) []string {
	return []string{
		"download the target miren release",
		fmt.Sprintf("replace %s with the new build (the previous one is kept as %s.old)", exe, exe),
	}
}

// sharesBinary reports whether exe is the daemon's binary, directly or
// through the /usr/local/bin symlink.
func sharesBinary(daemonBinary, exe string) bool {
	if exe == daemonBinary {
		return true
	}
	resolved, err := filepath.EvalSymlinks(daemonBinary)
	return err == nil && resolved == exe
}

// shellJoin renders argv the way a user would type it, so the command shown
// before the password prompt can be copied and run as-is.
func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	safe := s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !shellSafe(r)
	}) < 0
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("-_./=:,@+%", r)
}

// keepOwner lets a root install replace a CLI binary someone else owns, such
// as one under ~/.miren after a sudo re-run, without leaving it and its
// checksum owned by root; the owner's next non-root upgrade would then fail
// to write either. Call it before installing and the result afterwards.
func keepOwner(ctx *Context, path string) func() {
	fi, err := os.Stat(path)
	if err != nil {
		return func() {}
	}
	uid, gid, ok := ownerToRestore(fi, os.Geteuid())
	if !ok {
		return func() {}
	}
	return func() {
		for _, p := range []string{path, path + ".sha256"} {
			if err := os.Lchown(p, uid, gid); err != nil && !errors.Is(err, fs.ErrNotExist) {
				ctx.Warn("Could not give %s back to its owner (uid %d): %v", p, uid, err)
			}
		}
	}
}

// ownerToRestore reports the owner a root install should hand fi back to.
// A non-root install creates files as the caller, and a root-owned file needs
// nothing restored.
func ownerToRestore(fi fs.FileInfo, euid int) (uid, gid int, ok bool) {
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat || euid != 0 || st.Uid == 0 {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
