package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/release"
)

// UpgradeOptions contains options for the upgrade command
type UpgradeOptions struct {
	Version string `flag:"version" help:"Specific version to upgrade to (default: main)"`
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

		// Get metadata for the latest version
		downloader := release.NewDownloader()
		metadata, err := downloader.GetVersionMetadata(ctx, "main")
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		// Convert metadata to VersionInfo for comparison
		latestInfo := release.VersionInfo{
			Version:   metadata.Version,
			Commit:    metadata.Commit,
			BuildDate: metadata.BuildDate,
		}

		fmt.Printf("Current version: %s\n", current.Version)
		if current.Commit != "" && len(current.Commit) > 7 {
			fmt.Printf("Current commit:  %s\n", current.Commit[:7])
		}
		if !current.BuildDate.IsZero() {
			fmt.Printf("Current build:   %s\n", current.BuildDate.Format("2006-01-02 15:04:05 UTC"))
		}

		fmt.Printf("\nLatest version:  %s\n", latestInfo.Version)
		if latestInfo.Commit != "" && len(latestInfo.Commit) > 7 {
			fmt.Printf("Latest commit:   %s\n", latestInfo.Commit[:7])
		}
		if !latestInfo.BuildDate.IsZero() {
			fmt.Printf("Latest build:    %s\n", latestInfo.BuildDate.Format("2006-01-02 15:04:05 UTC"))
		}

		// Use proper version comparison with build dates
		if latestInfo.IsNewer(current) {
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
		version = "main" // Default to main branch
	}

	// Get metadata for the target version to check if it's actually newer
	if !opts.Force {
		downloader := release.NewDownloader()
		if metadata, err := downloader.GetVersionMetadata(ctx, version); err == nil {
			targetInfo := release.VersionInfo{
				Version:   metadata.Version,
				Commit:    metadata.Commit,
				BuildDate: metadata.BuildDate,
			}

			// Check if the target version is actually newer
			if !targetInfo.IsNewer(current) {
				if current.Version == targetInfo.Version {
					fmt.Printf("Already at version %s\n", version)
				} else {
					fmt.Printf("Current version %s is already up to date (target: %s)\n", current.Version, targetInfo.Version)
					if !current.BuildDate.IsZero() && !targetInfo.BuildDate.IsZero() {
						fmt.Printf("Current build: %s\n", current.BuildDate.Format("2006-01-02 15:04:05 UTC"))
						fmt.Printf("Target build:  %s\n", targetInfo.BuildDate.Format("2006-01-02 15:04:05 UTC"))
					}
				}
				return nil
			}
		}
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
