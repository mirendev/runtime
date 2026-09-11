//go:build linux

package commands

import (
	"miren.dev/runtime/pkg/servicelimits"
)

// serverStateDir is where the server keeps its own state, alongside the cloud
// registration. The ExecStopPost hook writes its exit record here.
const serverStateDir = "/var/lib/miren/server"

// installServiceLimits writes the managed resource-limit drop-in for a unit.
//
// It runs on every install, not only when the base unit is written, because the
// unit itself is left alone once it exists — so a host installed before this
// change would otherwise never gain a limit. The caller is responsible for the
// `systemctl daemon-reload` that makes it take effect.
//
// A failure here is reported but never fatal: an install that can't set a memory
// limit is worse off than one that can, but it still works.
func installServiceLimits(ctx *Context, unit, statePath string) {
	limits := servicelimits.Compute(servicelimits.DetectSystemRAMBytes())

	if err := servicelimits.Write(unit, statePath, limits); err != nil {
		ctx.Warn("Couldn't set resource limits for %s: %v", unit, err)
		return
	}

	if !limits.Known() {
		ctx.Warn("Couldn't detect system memory, so %s has no memory limit.", unit)
		return
	}
}

// removeServiceLimits deletes the managed drop-in during uninstall.
func removeServiceLimits(ctx *Context, unit string) {
	if err := servicelimits.Remove(unit); err != nil {
		ctx.Warn("Couldn't remove resource limits for %s: %v", unit, err)
	}
}
