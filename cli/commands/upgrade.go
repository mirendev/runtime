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
	Version string `short:"V" long:"version" description:"Specific version to upgrade to (e.g., v0.2.0)"`
	Channel string `long:"channel" description:"Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)"`
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

	version, err := resolveVersionChannel(opts.Version, opts.Channel)
	if err != nil {
		return err
	}

	// Detection needs no privileges; acting on it does.
	serverRunning := release.IsServerRunning()
	serverOpts := release.DefaultManagerOptions()

	if opts.Check {
		if serverRunning {
			return checkServerUpgrade(ctx, version, serverOpts)
		}
		return checkCLIUpgrade(ctx, version, exe)
	}

	if serverRunning && !opts.User {
		if os.Geteuid() != 0 {
			if !opts.Force {
				return errors.New("a miren server is running on this machine; re-run with sudo to upgrade it and the CLI together (or --force to upgrade only this CLI)")
			}
			ctx.Warn("Upgrading only the CLI; the running server needs 'sudo miren upgrade'.")
			return upgradeCLI(ctx, version, exe, opts.Force, false)
		}
		return upgradeServerAndCLI(ctx, version, exe, opts.Force, serverOpts, nil)
	}

	return upgradeCLI(ctx, version, exe, opts.Force, opts.User)
}

// upgradeServerAndCLI runs one operation for the server, then brings the
// invoking CLI binary onto the same build without a second download.
func upgradeServerAndCLI(ctx *Context, version, exe string, force bool, serverOpts release.ManagerOptions, customize func(*serverlifecycle.Operation)) error {
	current, _ := release.NewManager(serverOpts).GetCurrentVersion(ctx)

	needsUpgrade, err := CheckIfUpgradeNeeded(ctx, version, force, &serverOpts)
	if err != nil {
		ctx.Log.Warn("could not check version status", "error", err)
		needsUpgrade = true
	}

	var op *serverlifecycle.Operation
	if needsUpgrade {
		op = serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "cli")
		op.TargetVersion = version
	} else if drifted, err := serverDrifted(ctx, serverOpts); err == nil && drifted {
		ctx.Info("On-disk binary is current but the running server is older; restarting.")
		op = serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	}

	if op != nil {
		if customize != nil {
			customize(op)
		}
		ctx.Info("Upgrading server...")
		result, err := runLifecycleOperation(ctx, op)
		if err != nil {
			return err
		}
		if !result.Succeeded() {
			return fmt.Errorf("server upgrade %s: %s", result.Phase, result.Error)
		}
	} else {
		ctx.Info("Server is already up to date.")
	}

	if err := syncCLIBinary(ctx, serverOpts.InstallPath, exe); err != nil {
		return fmt.Errorf("server upgraded, but updating the CLI at %s failed: %w", exe, err)
	}

	newVersion, _ := release.NewManager(serverOpts).GetCurrentVersion(ctx)
	ctx.Printf("\nUpgrade successful:\n")
	ctx.Printf("  Server: %s -> %s (%s)\n", current.Display(), newVersion.Display(), serverOpts.InstallPath)
	ctx.Printf("  CLI:    %s (%s)\n", newVersion.Display(), exe)
	return nil
}

func serverDrifted(ctx *Context, serverOpts release.ManagerOptions) (bool, error) {
	running, err := release.GetRunningServiceVersion(serverOpts.ServiceName)
	if err != nil {
		return false, err
	}
	onDisk, err := release.NewManager(serverOpts).GetCurrentVersion(ctx)
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

func checkServerUpgrade(ctx *Context, version string, serverOpts release.ManagerOptions) error {
	current, latest, err := CheckVersionStatus(ctx, version, &serverOpts)
	if err != nil {
		return err
	}
	PrintVersionComparison(current, latest)
	drift := PrintRunningVersionDrift(serverOpts.ServiceName, current)

	switch {
	case latest.IsNewer(current):
		fmt.Println("\nAn update is available! Run 'sudo miren upgrade' to install it.")
	case drift:
		fmt.Println("\nOn-disk binary is current, but the running server is older. Run 'sudo miren upgrade' to restart it.")
	default:
		fmt.Println("\nYour server is already on the latest version.")
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
