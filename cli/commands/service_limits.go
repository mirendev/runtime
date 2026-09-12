//go:build linux

package commands

import (
	"path/filepath"

	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/servicelimits"
)

// defaultServerStateDir is where the server keeps its own state when nothing
// overrides the data path.
const defaultServerStateDir = "/var/lib/miren/server"

// serverStateDir resolves where the running server will look for its exit
// record.
//
// This has to agree with what the server itself uses, which is
// filepath.Join(Config.Server.GetDataPath(), "server"). A data_path set in
// /etc/miren/server.toml moves that directory, and a hook writing to the
// compiled-in default would then leave records somewhere the server never
// reads. The restart would go unreported, silently, on exactly the hosts whose
// operator cared enough to configure a data path.
//
// Falls back to the default when the config can't be loaded: an install that
// cannot read a config file has larger problems, and the default is right for
// almost every host.
func serverStateDir(ctx *Context) string {
	cfg, err := serverconfig.Load("", nil, ctx.Log)
	if err != nil || cfg == nil {
		return defaultServerStateDir
	}

	dataPath := cfg.Server.GetDataPath()
	if dataPath == "" {
		return defaultServerStateDir
	}
	return filepath.Join(dataPath, "server")
}

// installServiceLimits writes the managed resource-limit drop-in for a unit.
//
// It runs on every install, not only when the base unit is written, because the
// unit itself is left alone once it exists — so a host installed before this
// change would otherwise never gain a limit. The caller is responsible for the
// `systemctl daemon-reload` that makes it take effect.
//
// A failure here is reported but never fatal. An install that can't set a memory
// limit is worse off than one that can, but it still produces a working server,
// and refusing to install at all would be the larger regression. The warning
// says plainly what protection is missing so it can't pass for cosmetic.
func installServiceLimits(ctx *Context, unit, statePath string) {
	limits := servicelimits.Compute(servicelimits.DetectSystemRAMBytes())

	wrote, err := servicelimits.Write(unit, statePath, limits)
	if err != nil {
		ctx.Warn("Couldn't set resource limits for %s: %v", unit, err)
		ctx.Info("  The service is installed but unprotected: a runaway control process")
		ctx.Info("  can still exhaust this machine's memory and take the host down.")
		ctx.Info("  Set a limit by hand with: sudo systemctl edit %s", servicelimits.UnitShortName(unit))
		return
	}

	if !wrote {
		ctx.Warn("Couldn't detect system memory; keeping the existing limit for %s.", unit)
		return
	}

	if !limits.Known() {
		ctx.Warn("Couldn't detect system memory, so %s has no memory limit.", unit)
		ctx.Info("  Without one, a runaway control process can take the whole machine down.")
		ctx.Info("  Set one by hand with: sudo systemctl edit %s", servicelimits.UnitShortName(unit))
		return
	}

	ctx.Completed("Memory limit for %s set to %s", unit, formatBytes(limits.MemoryMaxBytes))
	ctx.Info("  Past this the kernel restarts miren rather than letting it exhaust the host.")
	ctx.Info("  Apps and addons run in their own cgroups and are not affected.")
	ctx.Info("  To change it: sudo systemctl edit %s", servicelimits.UnitShortName(unit))
}

// removeServiceLimits deletes the managed drop-in during uninstall.
func removeServiceLimits(ctx *Context, unit string) {
	if err := servicelimits.Remove(unit); err != nil {
		ctx.Warn("Couldn't remove resource limits for %s: %v", unit, err)
	}
}
