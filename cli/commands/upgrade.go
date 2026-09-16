package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// Upgrade upgrades the server and then the CLI on a systemd server host, and
// just the CLI anywhere else.
func Upgrade(ctx *Context, opts struct {
	Version string `short:"V" long:"version" description:"Specific version to upgrade to (e.g., v0.2.0). A runner host defaults to its coordinator's build"`
	Channel string `long:"channel" description:"Channel to use: 'latest' (stable releases, the default except on a runner host) or 'main' (bleeding edge)"`
	Check   bool   `short:"c" long:"check" description:"Check for available updates only"`
	Force   bool   `short:"f" long:"force" description:"Upgrade even if already up to date; without root, upgrade only the CLI even though a server is running"`
	User    bool   `short:"u" long:"user" description:"Install the CLI to ~/.miren/release/miren instead of the system location"`
}) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine current binary path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}

	// Detection needs no privileges; acting on it does. A host runs one
	// daemon or the other; the server is also a sandbox host in standalone
	// mode, so it wins if both units happen to be active.
	daemon, daemonRunning := runningDaemon()

	// The daemon's target is only wanted on the paths that check or act on
	// it. A runner's comes from the coordinator, which takes the runner's
	// own config to ask, and a CLI-only upgrade should not need that.
	targetFor := daemon
	if !opts.Check && (opts.User || os.Geteuid() != 0) {
		targetFor = lifecycleDaemon{}
	}
	version, exact, err := daemonTarget(ctx, targetFor, opts.Version, opts.Channel)
	if err != nil {
		return err
	}

	if opts.Check {
		if daemonRunning {
			return checkDaemonUpgrade(ctx, daemon, version, exact)
		}
		return checkCLIUpgrade(ctx, version, exe)
	}

	if daemonRunning && !opts.User {
		if os.Geteuid() != 0 {
			if !opts.Force {
				return fmt.Errorf("a miren %s is running on this machine; re-run with sudo to upgrade it and the CLI together (or --force to upgrade only this CLI)", daemon.name)
			}
			ctx.Warn("Upgrading only the CLI; the running %s needs 'sudo miren upgrade'.", daemon.name)
			return upgradeCLI(ctx, version, exe, opts.Force, false)
		}
		return upgradeDaemonAndCLI(ctx, daemon, version, exact, exe, opts.Force, nil)
	}

	return upgradeCLI(ctx, version, exe, opts.Force, opts.User)
}

// runningDaemon reports which daemon this host runs, if any.
func runningDaemon() (lifecycleDaemon, bool) {
	switch {
	case release.IsServerRunning():
		return serverDaemon, true
	case release.IsRunnerRunning():
		return runnerDaemon, true
	}
	return lifecycleDaemon{}, false
}

// upgradeDaemonAndCLI runs one operation for the daemon, then brings the
// invoking CLI binary onto the same build without a second download.
func upgradeDaemonAndCLI(ctx *Context, daemon lifecycleDaemon, version string, exact bool, exe string, force bool, customize func(*serverlifecycle.Operation)) error {
	mgrOpts := daemon.manager()
	current, _ := release.NewManager(mgrOpts).GetCurrentVersion(ctx)

	needsUpgrade, err := upgradeNeededFor(ctx, version, exact, force, &mgrOpts)
	if err != nil {
		ctx.Log.Warn("could not check version status", "error", err)
		needsUpgrade = true
	}

	var op *serverlifecycle.Operation
	if needsUpgrade {
		op = serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "cli")
		op.TargetVersion = version
	} else if drifted, err := daemonDrifted(ctx, mgrOpts); err == nil && drifted {
		ctx.Info("On-disk binary is current but the running %s is older; restarting.", daemon.name)
		op = serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	}

	if op != nil {
		if customize != nil {
			customize(op)
		}
		ctx.Info("Upgrading %s...", daemon.name)
		result, err := runOperation(ctx, daemon, op)
		if err != nil {
			return err
		}
		if !result.Succeeded() {
			return fmt.Errorf("%s upgrade %s: %s", daemon.name, result.Phase, result.Error)
		}
		op = result
	} else {
		ctx.Info("%s is already up to date.", daemon.title())
	}

	if err := syncCLIBinary(ctx, mgrOpts.InstallPath, exe); err != nil {
		return fmt.Errorf("%s upgraded, but updating the CLI at %s failed: %w", daemon.name, exe, err)
	}

	newVersion, _ := release.NewManager(mgrOpts).GetCurrentVersion(ctx)
	ctx.Printf("\nUpgrade successful:\n")
	ctx.Printf("  %-7s %s -> %s (%s)\n", daemon.title()+":", current.Display(), newVersion.Display(), mgrOpts.InstallPath)
	ctx.Printf("  %-7s %s (%s)\n", "CLI:", newVersion.Display(), exe)
	if op != nil {
		for _, step := range op.Nodes {
			ctx.Printf("  Runner  %s: %s\n", step.Name, describeStep(step))
		}
	}
	return nil
}

func daemonDrifted(ctx *Context, mgrOpts release.ManagerOptions) (bool, error) {
	running, err := release.GetRunningServiceVersion(mgrOpts.ServiceName)
	if err != nil {
		return false, err
	}
	onDisk, err := release.NewManager(mgrOpts).GetCurrentVersion(ctx)
	if err != nil {
		return false, err
	}
	return !running.Equivalent(onDisk), nil
}

// syncCLIBinary is a no-op when exe already is the server binary, directly or
// through the /usr/local/bin symlink.
func syncCLIBinary(ctx *Context, serverBinary, exe string) error {
	if exe == serverBinary {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(serverBinary); err == nil && resolved == exe {
		return nil
	}
	serverVer, err := release.GetCurrentVersion(serverBinary)
	if err != nil {
		return err
	}
	if cliVer, err := release.GetCurrentVersion(exe); err == nil && cliVer.Equivalent(serverVer) {
		return nil
	}

	ctx.Info("Updating CLI at %s...", exe)
	staged, err := os.CreateTemp(filepath.Dir(exe), ".miren-upgrade-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	src, err := os.Open(serverBinary)
	if err != nil {
		staged.Close()
		os.Remove(stagedPath)
		return err
	}
	_, copyErr := staged.ReadFrom(src)
	src.Close()
	if closeErr := staged.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(stagedPath)
		return copyErr
	}
	installer := release.NewInstaller(release.InstallOptions{InstallPath: exe, BackupSuffix: ".old"})
	if err := installer.Install(ctx, &release.DownloadedArtifact{Path: stagedPath}); err != nil {
		os.Remove(stagedPath)
		return err
	}
	return nil
}

func checkDaemonUpgrade(ctx *Context, daemon lifecycleDaemon, version string, exact bool) error {
	mgrOpts := daemon.manager()
	current, latest, err := CheckVersionStatus(ctx, version, &mgrOpts)
	if err != nil {
		return err
	}
	PrintVersionComparison(current, latest)
	drift := PrintRunningVersionDrift(mgrOpts.ServiceName, current)

	switch {
	case exact && !current.Equivalent(latest) && !latest.IsNewer(current):
		fmt.Printf("\nThis %s is ahead of its coordinator (%s). Run 'sudo miren upgrade' to match it.\n", daemon.name, latest.Version)
	case latest.IsNewer(current):
		fmt.Println("\nAn update is available! Run 'sudo miren upgrade' to install it.")
	case drift:
		fmt.Printf("\nOn-disk binary is current, but the running %s is older. Run 'sudo miren upgrade' to restart it.\n", daemon.name)
	default:
		fmt.Printf("\nYour %s is already on the latest version.\n", daemon.name)
	}
	return nil
}

func checkCLIUpgrade(ctx *Context, version, exe string) error {
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.InstallPath = exe
	mgrOpts.SkipHealthCheck = true
	current, latest, err := CheckVersionStatus(ctx, version, &mgrOpts)
	if err != nil {
		return err
	}
	PrintVersionComparison(current, latest)
	if latest.IsNewer(current) {
		fmt.Println("\nAn update is available! Run 'miren upgrade' to install it.")
	} else {
		fmt.Println("\nYou are already on the latest version.")
	}
	return nil
}

// upgradeCLI is the client-only path.
func upgradeCLI(ctx *Context, version, exe string, force, user bool) error {
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.InstallPath = exe
	mgrOpts.SkipHealthCheck = true
	mgrOpts.PathSymlink = ""

	installPath := exe
	isUserInstall := user
	if user {
		userPath, err := getUserMirenPath()
		if err != nil {
			return fmt.Errorf("failed to determine user install path: %w", err)
		}
		installPath = userPath
	} else if err := checkInstallPermissions(installPath); err != nil {
		permErr, ok := errors.AsType[*permissionError](err)
		if !ok {
			return fmt.Errorf("permission check failed: %w", err)
		}
		option, handleErr := handlePermissionError(ctx, installPath, permErr)
		if handleErr != nil {
			return handleErr
		}
		switch option {
		case upgradeOptionSudo:
			ctx.Info("")
			ctx.Info("Please re-run with: sudo miren upgrade")
			return nil
		case upgradeOptionUser:
			userPath, err := getUserMirenPath()
			if err != nil {
				return fmt.Errorf("failed to determine user install path: %w", err)
			}
			installPath = userPath
			isUserInstall = true
		case upgradeOptionCancel:
			return nil
		}
	}

	if isUserInstall {
		needsPathUpdate, err := ensureUserInstallDir(installPath)
		if err != nil {
			return err
		}
		if needsPathUpdate {
			ctx.Warn("Note: %s is not in your PATH", filepath.Dir(installPath))
			ctx.Info("Add it to your shell configuration to use 'miren' directly:")
			ctx.Info("  export PATH=\"%s:$PATH\"", filepath.Dir(installPath))
			ctx.Info("")
		}
		if exe != installPath {
			if _, err := os.Stat(exe); err == nil {
				ctx.Warn("Note: %s still exists and may take precedence over the user install", exe)
				ctx.Info("You may need to remove it or ensure %s comes first in your PATH", filepath.Dir(installPath))
				ctx.Info("")
			}
		}
	}

	mgrOpts.InstallPath = installPath
	mgr := release.NewManager(mgrOpts)

	needsUpgrade, err := CheckIfUpgradeNeeded(ctx, version, force, &mgrOpts)
	if err != nil {
		ctx.Log.Warn("could not check version status", "error", err)
	} else if !needsUpgrade {
		return nil
	}

	current, _ := mgr.GetCurrentVersion(ctx)
	artifact := release.NewArtifact(release.ArtifactTypeBinary, version)
	if err := mgr.UpgradeArtifact(ctx, artifact); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	PrintUpgradeSuccess(ctx, current, "CLI", &mgrOpts)
	return nil
}
