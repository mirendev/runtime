//go:build linux

package commands

import (
	"errors"
	"os"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverinfo"
)

// healPathSymlinkAtBoot points /usr/local/bin/miren at the managed release
// binary from inside the daemon, where the code is always current no matter
// which binary drove the last upgrade. Systemd installs only: in the container
// image /usr/local/bin/miren is the image's own binary, the one container-boot
// falls back to when the volume's cannot start, and a symlink into the volume
// would take that fallback away. Best-effort: a symlink problem must never
// keep a server or runner from starting.
func healPathSymlinkAtBoot(ctx *Context) {
	if !pathSymlinkHealAllowed() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		ctx.Log.Warn("could not determine running executable for CLI symlink healing", "error", err)
		return
	}
	installPath := release.DefaultManagerOptions().InstallPath
	switch healed, err := release.HealPathSymlink(exe, installPath, release.SystemCLIPath); {
	case errors.Is(err, release.ErrPathManagedElsewhere):
		ctx.Log.Info("leaving CLI path alone, managed by another tool", "path", release.SystemCLIPath)
	case err != nil:
		ctx.Log.Warn("could not update CLI symlink", "path", release.SystemCLIPath, "error", err)
	case healed:
		ctx.Log.Info("linked CLI path to managed binary", "path", release.SystemCLIPath, "target", installPath)
	}
}

// pathSymlinkHealAllowed is the supervision gate: systemd installs only, and
// never a container-boot process. Container-boot is checked on its own rather
// than through DetectInstallKind alone, because that prefers systemd when both
// markers are set, which is right for restarts and wrong here.
func pathSymlinkHealAllowed() bool {
	if os.Getenv(serverinfo.ContainerBootEnv) != "" {
		return false
	}
	return serverinfo.DetectInstallKind() == serverinfo.InstallKindSystemd
}
