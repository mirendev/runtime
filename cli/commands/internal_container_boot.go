//go:build linux

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"miren.dev/runtime/pkg/containerboot"
)

// InternalContainerBoot is the container image's entrypoint for `server`. It
// runs from the image's own binary, decides which binary the volume should
// boot, and execs it with the arguments it was given. Exec, not spawn: the
// server must stay tini's direct child so signals and exit codes reach it
// the way they do today.
func InternalContainerBoot(ctx *Context, opts struct {
	ReleaseDir   string   `long:"release-dir" description:"Release directory in the data volume" default:"/var/lib/miren/release"`
	LifecycleDir string   `long:"lifecycle-dir" description:"Operation ledger to check for a crash-looping upgrade" default:"/var/lib/miren/server/lifecycle"`
	Args         []string `rest:"true"`
}) error {
	if len(opts.Args) == 0 {
		return fmt.Errorf("container-boot needs the miren arguments to exec, e.g. `-- server`")
	}
	image, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate image binary: %w", err)
	}
	if image, err = filepath.EvalSymlinks(image); err != nil {
		return fmt.Errorf("locate image binary: %w", err)
	}

	boot := containerboot.Boot{ReleaseDir: opts.ReleaseDir, ImageBinary: image, LifecycleDir: opts.LifecycleDir, Log: ctx.Log}
	bin, err := boot.Prepare(ctx)
	if err != nil {
		// The image binary is the one thing known to work, so a boot that
		// cannot sort out the volume runs it rather than nothing.
		ctx.Log.Error("could not prepare the release directory; booting the image's own binary", "error", err)
		bin = image
	} else if op := boot.GuardUpgrade(ctx); op != nil {
		// The watchdog is the only thing that will notice this build
		// hanging; if it cannot start, the boot still goes ahead and the
		// executor's own deadline or the next boot's attempt count is
		// what is left.
		cmd := exec.Command(image, "internal", "container-watchdog",
			"--operation", op.ID, "--server-pid", strconv.Itoa(os.Getpid()),
			"--release-dir", opts.ReleaseDir, "--lifecycle-dir", opts.LifecycleDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			ctx.Log.Error("could not start the upgrade watchdog", "operation", op.ID, "error", err)
		}
	}

	if err := syscall.Exec(bin, append([]string{bin}, opts.Args...), os.Environ()); err != nil {
		if bin == image {
			return fmt.Errorf("exec %s: %w", bin, err)
		}
		ctx.Log.Error("could not exec the release binary; booting the image's own binary", "path", bin, "error", err)
		if err := syscall.Exec(image, append([]string{image}, opts.Args...), os.Environ()); err != nil {
			return fmt.Errorf("exec %s: %w", image, err)
		}
	}
	return nil
}
