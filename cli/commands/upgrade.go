package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/release"
)

// Upgrade upgrades the miren CLI to the latest or specified version
func Upgrade(ctx *Context, opts struct {
	Version string `short:"V" long:"version" description:"Specific version to upgrade to (e.g., v0.2.0)"`
	Channel string `long:"channel" description:"Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)"`
	Check   bool   `short:"c" long:"check" description:"Check for available updates only"`
	Force   bool   `short:"f" long:"force" description:"Force upgrade even if already up to date or server running"`
	User    bool   `short:"u" long:"user" description:"Install to user directory (~/.miren/release/miren) instead of system location"`
}) error {
	// Determine current binary path for version checking
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine current binary path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}

	// Create manager for version checking
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.InstallPath = exe
	mgrOpts.SkipHealthCheck = true // CLI doesn't need health check

	version, err := resolveVersionChannel(opts.Version, opts.Channel)
	if err != nil {
		return err
	}

	// Check mode - just report if update is available
	if opts.Check {
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

	// From here on, we're doing an actual upgrade

	// Check if server is running (unless forced)
	if !opts.Force && release.IsServerRunning() {
		return fmt.Errorf("miren server is running. Use 'sudo miren server upgrade' to upgrade the server, or use --force to upgrade the CLI anyway")
	}

	// Determine installation path
	var installPath string
	isUserInstall := opts.User

	if opts.User {
		// User explicitly requested user directory installation
		userPath, err := getUserMirenPath()
		if err != nil {
			return fmt.Errorf("failed to determine user install path: %w", err)
		}
		installPath = userPath
	} else {
		installPath = exe
	}

	// Check permissions before downloading (skip if user already requested --user)
	if !opts.User {
		if err := checkInstallPermissions(installPath); err != nil {
			if permErr, ok := errors.AsType[*permissionError](err); ok {
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
			} else {
				return fmt.Errorf("permission check failed: %w", err)
			}
		}
	}

	// Ensure the install directory exists for user installs
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

		// Warn if there's an existing system binary that might take precedence
		if exe != installPath {
			if _, err := os.Stat(exe); err == nil {
				ctx.Warn("Note: %s still exists and may take precedence over the user install", exe)
				ctx.Info("You may need to remove it or ensure %s comes first in your PATH", filepath.Dir(installPath))
				ctx.Info("")
			}
		}
	}

	// Update manager with final install path
	mgrOpts.InstallPath = installPath
	mgr := release.NewManager(mgrOpts)

	needsUpgrade, err := CheckIfUpgradeNeeded(ctx, version, opts.Force, &mgrOpts)
	if err != nil {
		ctx.Log.Warn("could not check version status", "error", err)
		// Continue with upgrade if we can't check
	} else if !needsUpgrade {
		return nil // Already up to date
	}

	// Get current version for comparison after upgrade
	current, _ := mgr.GetCurrentVersion(ctx)

	// Create artifact descriptor - use binary type for CLI upgrades (just the miren binary)
	// Binary artifacts are .tar.gz files available for all platforms
	artifact := release.NewArtifact(release.ArtifactTypeBinary, version)

	// Perform upgrade
	if err := mgr.UpgradeArtifact(ctx, artifact); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	// Report success
	PrintUpgradeSuccess(ctx, current, "CLI", &mgrOpts)

	return nil
}
