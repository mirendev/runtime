//go:build linux

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"miren.dev/runtime/pkg/containerboot"
)

// InternalContainerWatchdog runs the watchdog for one boot of an upgraded
// build; see containerboot.Watchdog. Started by container-boot right before
// it execs the server.
//
// It runs detached: the first invocation starts a second copy of itself and
// returns, so the watchdog ends up a child of tini rather than of the
// server, where it would sit as a zombie until the server exits.
func InternalContainerWatchdog(ctx *Context, opts struct {
	Operation    string `long:"operation" required:"true" description:"Upgrade operation to watch"`
	ServerPID    int    `long:"server-pid" required:"true" description:"PID of the server this boot runs"`
	ReleaseDir   string `long:"release-dir" description:"Release directory in the data volume" default:"/var/lib/miren/release"`
	LifecycleDir string `long:"lifecycle-dir" description:"Operation ledger" default:"/var/lib/miren/server/lifecycle"`
	Detached     bool   `long:"detached" description:"Run the watchdog in this process (set by the first invocation)"`
}) error {
	if !opts.Detached {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(self, "internal", "container-watchdog",
			"--operation", opts.Operation, "--server-pid", strconv.Itoa(opts.ServerPID),
			"--release-dir", opts.ReleaseDir, "--lifecycle-dir", opts.LifecycleDir, "--detached")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Start()
	}
	image, err := os.Executable()
	if err != nil {
		return err
	}
	if image, err = filepath.EvalSymlinks(image); err != nil {
		return err
	}
	w := containerboot.Watchdog{
		Boot:        containerboot.Boot{ReleaseDir: opts.ReleaseDir, ImageBinary: image, LifecycleDir: opts.LifecycleDir, Log: ctx.Log},
		OperationID: opts.Operation,
		ServerPID:   opts.ServerPID,
	}
	if err := w.Run(ctx); err != nil {
		return fmt.Errorf("container watchdog: %w", err)
	}
	return nil
}
