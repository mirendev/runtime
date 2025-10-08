package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/release"
)

// Upgrade upgrades the miren CLI to the latest or specified version
func Upgrade(ctx *Context, opts struct {
	Version string `short:"v" long:"version" description:"Specific version to upgrade to (default: main)"`
	Check   bool   `short:"c" long:"check" description:"Check for available updates only"`
	Force   bool   `short:"f" long:"force" description:"Force upgrade even if already up to date or server running"`
}) error {
	// Check if server is running (unless forced or just checking)
	if !opts.Force && !opts.Check && release.IsServerRunning() {
		return fmt.Errorf("miren server is running. Use 'sudo miren server upgrade' to upgrade the server, or use --force to upgrade the CLI anyway")
	}

	// Determine installation path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine current binary path: %w", err)
	}

	// Resolve any symlinks
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}

	// Create manager with appropriate install path
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.InstallPath = exe
	mgrOpts.SkipHealthCheck = true // CLI doesn't need health check
	mgr := release.NewManager(mgrOpts)

	// Check mode - just report if update is available
	if opts.Check {
		current, latest, err := CheckVersionStatus(ctx, opts.Version, &mgrOpts)
		if err != nil {
			return err
		}

		PrintVersionComparison(current, latest)

		// Use proper version comparison with build dates
		if latest.IsNewer(current) {
			fmt.Println("\nAn update is available! Run 'miren upgrade' to install it.")
		} else {
			fmt.Println("\nYou are already on the latest version.")
		}
		return nil
	}

	// Check if upgrade is needed (unless forced)
	version := opts.Version
	if version == "" {
		version = "main" // Default to main branch
	}

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
	// Binary artifacts are .zip files available for all platforms
	artifact := release.NewArtifact(release.ArtifactTypeBinary, version)

	// Perform upgrade
	if err := mgr.UpgradeArtifact(ctx, artifact); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	// Report success
	PrintUpgradeSuccess(ctx, current, "CLI", &mgrOpts)

	return nil
}
