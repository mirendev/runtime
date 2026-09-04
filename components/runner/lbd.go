package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/lbdmod/ctrbuild"
)

// rebuildTimeout bounds the unattended rebuild at startup.
//
// A rebuild pulls the builder image, may fetch kernel headers, and compiles;
// on a real node that is tens of seconds, and this leaves generous room for a
// slow link. What it must not do is wait forever: without a bound, a builder
// that wedges -- a stalled fetch, a hung pull -- holds up the runner's whole
// startup with no way out, which is a far worse outcome than the slower disks
// we get by giving up.
const rebuildTimeout = 10 * time.Minute

// setupLbd brings accelerator mode up, rebuilding the lbd kernel module if a
// kernel upgrade left the installed one unusable.
//
// A module only loads on the kernel it was built for, so an operator who
// enabled accelerator mode and then took a kernel update would otherwise find
// their disks silently back on loop devices. Rebuilding is limited to hosts
// that already installed the module: a host that never opted in should not pay
// for an unattended compile at startup.
//
// This blocks rather than running in the background because the disk
// controller picks universal or accelerator mode once, at startup, from
// whether lbd is usable. Deciding that before the module is ready would pin
// the node to loop devices until the next restart.
//
// Neither failing nor timing out is fatal. Universal mode works everywhere, so
// the worst case is slower disks, not a runner that will not start.
func setupLbd(ctx context.Context, cc *containerd.Client, dataPath string, log *slog.Logger) {
	if err := diskio.EnsureLbdDevices(ctx, log); err == nil {
		return
	}

	// dataPath has to be the runner's own, not the package default: the
	// install record lives under it, and reading it from the wrong place
	// would make a host that installed lbd look like one that never did, so
	// the rebuild after a kernel upgrade would never fire.
	installer := &lbdmod.Installer{
		Log:     log,
		Builder: ctrbuild.New(cc, log),
		Options: lbdmod.HostOptions(dataPath),
	}

	ctx, cancel := context.WithTimeout(ctx, rebuildTimeout)
	defer cancel()

	rebuilt, err := installer.EnsureCurrent(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		log.Warn("gave up rebuilding the lbd kernel module, disks will use loop devices",
			"timeout", rebuildTimeout,
			"retry_with", "miren disk accelerator install")
		return
	}
	if err != nil {
		log.Warn("could not rebuild the lbd kernel module, disks will use loop devices", "error", err)
		return
	}
	if !rebuilt {
		// Nothing to rebuild: this host never installed the module.
		log.Info("accelerator mode is not enabled on this host, disks will use loop devices",
			"enable_with", "miren disk accelerator install")
		return
	}

	if err := diskio.EnsureLbdDevices(ctx, log); err != nil {
		log.Warn("rebuilt the lbd kernel module but it is still not usable", "error", err)
	}
}
