package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/release"
)

// UpgradeOptions contains options for the upgrade command
type UpgradeOptions struct {
	Version string `flag:"version" help:"Specific version to upgrade to (default: latest)"`
	Check   bool   `flag:"check" help:"Check for available updates only"`
	Force   bool   `flag:"force" help:"Force upgrade even if already up to date or server running"`
}

// Upgrade upgrades the miren CLI to the latest or specified version
func Upgrade(ctx *Context, opts UpgradeOptions) error {
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
		current, err := mgr.GetCurrentVersion(ctx)
		if err != nil {
			return fmt.Errorf("failed to get current version: %w", err)
		}

		latest, err := mgr.GetLatestVersion(ctx, release.ArtifactTypeBase)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		fmt.Printf("Current version: %s\n", current.Version)
		fmt.Printf("Latest version:  %s\n", latest)

		// Try to fetch detailed metadata for more info
		downloader := release.NewDownloader()
		if metadata, err := downloader.GetVersionMetadata(ctx, "main"); err == nil {
			if metadata.Commit != "" && current.Commit != "" && len(metadata.Commit) > 7 && len(current.Commit) > 7 {
				if metadata.Commit[:7] != current.Commit[:7] {
					fmt.Printf("Latest commit:   %s\n", metadata.Commit[:7])
					if metadata.BuildDate.IsZero() == false {
						fmt.Printf("Build date:      %s\n", metadata.BuildDate.Format("2006-01-02 15:04:05 UTC"))
					}
				}
			}
		}

		if current.Version != latest {
			fmt.Println("\nAn update is available! Run 'miren upgrade' to install it.")
		} else {
			fmt.Println("\nYou are already on the latest version.")
		}
		return nil
	}

	// Check current version
	current, _ := mgr.GetCurrentVersion(ctx)

	// Determine target version
	version := opts.Version
	if version == "" {
		version = "main" // Default to main branch for now
	}

	// Check if already up to date (unless forced)
	if !opts.Force && current.Version == version {
		fmt.Printf("Already at version %s\n", version)
		return nil
	}

	// Create artifact descriptor - use binary type for CLI upgrades (just the miren binary)
	// Binary artifacts are .zip files available for all platforms
	artifact := release.NewArtifact(release.ArtifactTypeBinary, version)

	// Perform upgrade
	if err := mgr.UpgradeArtifact(ctx, artifact); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	// Report success
	newVersion, _ := mgr.GetCurrentVersion(ctx)
	if current.Version != "" {
		fmt.Printf("\nUpgrade successful:\n")
		fmt.Printf("  Old: %s", current.Version)
		if c := current.Commit; c != "" && c != "unknown" {
			end := 8
			if len(c) < end {
				end = len(c)
			}
			fmt.Printf(" (%s)", c[:end])
		}
		fmt.Printf("\n  New: %s", newVersion.Version)
		if c := newVersion.Commit; c != "" && c != "unknown" {
			end := 8
			if len(c) < end {
				end = len(c)
			}
			fmt.Printf(" (%s)", c[:end])
		}
		fmt.Printf("\n")
	} else {
		fmt.Printf("\nInstalled version %s successfully\n", newVersion.Version)
	}

	return nil
}
