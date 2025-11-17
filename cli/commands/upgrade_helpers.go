package commands

import (
	"context"
	"fmt"

	"miren.dev/runtime/pkg/release"
)

// CheckVersionStatus checks if an update is available for the given target version
// Returns the current and latest version info
// If mgrOpts is nil, uses default manager options (for server path)
func CheckVersionStatus(ctx context.Context, targetVersion string, mgrOpts *release.ManagerOptions) (current, latest release.VersionInfo, err error) {
	opts := release.DefaultManagerOptions()
	if mgrOpts != nil {
		opts = *mgrOpts
	}
	mgr := release.NewManager(opts)
	current, _ = mgr.GetCurrentVersion(ctx)

	if targetVersion == "" {
		targetVersion = "latest"
	}

	downloader := release.NewDownloader()
	metadata, err := downloader.GetVersionMetadata(ctx, targetVersion)
	if err != nil {
		return current, latest, fmt.Errorf("failed to check for updates: %w", err)
	}

	latest = release.VersionInfo{
		Version:   metadata.Version,
		Commit:    metadata.Commit,
		BuildDate: metadata.BuildDate,
	}

	// If version is a semver prerelease, warn user
	if semver, err := release.ParseSemVer(latest.Version); err == nil && semver.IsPrerelease() {
		fmt.Printf("⚠️  Warning: %s is a prerelease version\n", latest.Version)
	}

	return current, latest, nil
}

// PrintVersionComparison prints a formatted comparison of current vs latest versions
func PrintVersionComparison(current, latest release.VersionInfo) {
	fmt.Printf("Current version: %s\n", current.Version)
	if current.Commit != "" && len(current.Commit) > 7 {
		fmt.Printf("Current commit:  %s\n", current.Commit[:7])
	}
	if !current.BuildDate.IsZero() {
		fmt.Printf("Current build:   %s\n", current.BuildDate.Format("2006-01-02 15:04:05 UTC"))
	}

	fmt.Printf("\nLatest version:  %s\n", latest.Version)
	if latest.Commit != "" && len(latest.Commit) > 7 {
		fmt.Printf("Latest commit:   %s\n", latest.Commit[:7])
	}
	if !latest.BuildDate.IsZero() {
		fmt.Printf("Latest build:    %s\n", latest.BuildDate.Format("2006-01-02 15:04:05 UTC"))
	}
}

// CheckIfUpgradeNeeded checks if target version is newer than current
// Returns true if upgrade is needed, false if already up to date
// If mgrOpts is nil, uses default manager options (for server path)
func CheckIfUpgradeNeeded(ctx context.Context, targetVersion string, force bool, mgrOpts *release.ManagerOptions) (bool, error) {
	if force {
		return true, nil
	}

	current, latest, err := CheckVersionStatus(ctx, targetVersion, mgrOpts)
	if err != nil {
		// If we can't check, allow upgrade to proceed
		return true, nil
	}

	// Check if the target version is actually newer
	if !latest.IsNewer(current) {
		if current.Version == latest.Version {
			fmt.Printf("Already at version %s\n", targetVersion)
		} else {
			fmt.Printf("Current version %s is already up to date (target: %s)\n", current.Version, latest.Version)
			if !current.BuildDate.IsZero() && !latest.BuildDate.IsZero() {
				fmt.Printf("Current build: %s\n", current.BuildDate.Format("2006-01-02 15:04:05 UTC"))
				fmt.Printf("Target build:  %s\n", latest.BuildDate.Format("2006-01-02 15:04:05 UTC"))
			}
		}
		return false, nil
	}

	return true, nil
}

// PrintUpgradeSuccess prints a formatted success message after upgrade
// If mgrOpts is nil, uses default manager options (for server path)
func PrintUpgradeSuccess(ctx context.Context, oldVersion release.VersionInfo, commandType string, mgrOpts *release.ManagerOptions) {
	opts := release.DefaultManagerOptions()
	if mgrOpts != nil {
		opts = *mgrOpts
	}
	mgr := release.NewManager(opts)
	newVersion, _ := mgr.GetCurrentVersion(ctx)

	if oldVersion.Version != "" {
		fmt.Printf("\n%s upgrade successful:\n", commandType)
		fmt.Printf("  Old: %s", oldVersion.Version)
		if c := oldVersion.Commit; c != "" && c != "unknown" {
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
	}
}
