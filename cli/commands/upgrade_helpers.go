package commands

import (
	"context"
	"fmt"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/runnerlifecycle"
	"miren.dev/runtime/version"
)

// resolveVersionChannel collapses the --version and --channel flags into a
// single target version string. The two flags are mutually exclusive; when
// neither is set the target defaults to the "latest" channel.
// daemonTarget is the version a daemon on this host is upgraded to. With
// neither flag, a server follows the latest release, and a runner follows its
// coordinator: the fleet converges on the coordinator's build, and a runner
// upgraded from its own host (the one-time step that brings a runner onto
// managed upgrades, or an operator fixing one by hand) must not get ahead of
// it. The flags are the explicit way to say otherwise.
//
// The bool reports an exact match: the coordinator runs a tagged release and
// the target is that tag. An exact match is a two-way street, since a runner
// that got ahead of its coordinator is just as much out of step as one
// behind it; the walk from the coordinator treats it the same way.
func daemonTarget(ctx *Context, daemon lifecycleDaemon, versionFlag, channelFlag string) (target string, exact bool, err error) {
	if versionFlag != "" || channelFlag != "" || daemon.name != "runner" {
		target, err = resolveVersionChannel(versionFlag, channelFlag)
		return target, false, err
	}
	cfg, err := runnerconfig.Load("")
	if err != nil {
		return "", false, fmt.Errorf("read runner config to find the coordinator: %w (pass --version or --channel to choose a target directly)", err)
	}
	coordinator, err := runnerlifecycle.CoordinatorVersion(ctx, cfg, ctx.Log)
	if err != nil {
		return "", false, fmt.Errorf("could not ask the coordinator at %s which build to match: %w (pass --version or --channel to choose a target directly)", cfg.CoordinatorAddress, err)
	}
	target = version.BranchOf(coordinator)
	if target == "" {
		return "", false, fmt.Errorf("coordinator at %s reports version %q, which names no build to match (pass --version or --channel)", cfg.CoordinatorAddress, coordinator)
	}
	if target == coordinator {
		ctx.Info("Matching the coordinator: %s", coordinator)
		return target, true, nil
	}
	// A branch build resolves to its channel, so "latest of main" is the
	// closest the runner can get; the walk from the coordinator checks the
	// result against its own build.
	ctx.Info("Matching the coordinator (%s) from the %s channel", coordinator, target)
	return target, false, nil
}

// upgradeNeededFor is CheckIfUpgradeNeeded with the exact-match rule: a
// target that must be matched is needed whenever the installed build is a
// different one, newer included.
func upgradeNeededFor(ctx context.Context, target string, exact, force bool, mgrOpts *release.ManagerOptions) (bool, error) {
	if !exact {
		return CheckIfUpgradeNeeded(ctx, target, force, mgrOpts)
	}
	if force {
		return true, nil
	}
	current, wanted, err := CheckVersionStatus(ctx, target, mgrOpts)
	if err != nil {
		return true, nil
	}
	if current.Equivalent(wanted) {
		fmt.Printf("Already at version %s\n", target)
		return false, nil
	}
	if current.IsNewer(wanted) {
		fmt.Printf("Installed %s is ahead of the coordinator's %s; matching the coordinator\n", current.Version, wanted.Version)
	}
	return true, nil
}

func resolveVersionChannel(version, channel string) (string, error) {
	if version != "" && channel != "" {
		return "", fmt.Errorf("--version and --channel are mutually exclusive")
	}
	if version == "" && channel == "" {
		return "latest", nil
	}
	if channel != "" {
		return channel, nil
	}
	return version, nil
}

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
	if sc := current.ShortCommit(); sc != "" {
		fmt.Printf("Current commit:  %s\n", sc)
	}
	if !current.BuildDate.IsZero() {
		fmt.Printf("Current build:   %s\n", current.BuildDate.UTC().Format("2006-01-02 15:04:05 UTC"))
	}

	fmt.Printf("\nLatest version:  %s\n", latest.Version)
	if sc := latest.ShortCommit(); sc != "" {
		fmt.Printf("Latest commit:   %s\n", sc)
	}
	if !latest.BuildDate.IsZero() {
		fmt.Printf("Latest build:    %s\n", latest.BuildDate.UTC().Format("2006-01-02 15:04:05 UTC"))
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
				fmt.Printf("Current build: %s\n", current.BuildDate.UTC().Format("2006-01-02 15:04:05 UTC"))
				fmt.Printf("Target build:  %s\n", latest.BuildDate.UTC().Format("2006-01-02 15:04:05 UTC"))
			}
		}
		return false, nil
	}

	return true, nil
}

// PrintRunningVersionDrift prints an additional line about the running daemon
// when its version differs from the on-disk binary. Used by --check paths to
// surface drift without trying to upgrade. Returns true when drift was
// reported, so callers can adjust their conclusion text.
func PrintRunningVersionDrift(serviceName string, onDisk release.VersionInfo) bool {
	runningVer, err := release.GetRunningServiceVersion(serviceName)
	if err != nil {
		return false
	}
	if runningVer.Equivalent(onDisk) {
		return false
	}
	fmt.Printf("\nRunning daemon:  %s\n", runningVer.Display())
	return true
}

// HandleVersionDrift checks whether the running daemon for the given systemd
// service differs from the binary on disk. If they differ, the service is
// restarted via mgr (no download, no install) and the function returns true.
//
// This exists because `CheckIfUpgradeNeeded` only compares on-disk binary vs
// target version. If a previous `miren upgrade` already replaced the on-disk
// binary, the running daemon is stale even though "current == target". Drift
// detection catches that case and triggers just the restart.
//
// If the running version can't be determined (service inaccessible, /proc not
// readable, etc.), we log a note and return (false, nil) rather than forcing a
// surprise restart.
func HandleVersionDrift(ctx context.Context, mgr *release.Manager, serviceName string) (bool, error) {
	runningVer, err := release.GetRunningServiceVersion(serviceName)
	if err != nil {
		fmt.Printf("Note: could not determine running %s version (%v); skipping drift check\n", serviceName, err)
		return false, nil
	}

	onDiskVer, err := mgr.GetCurrentVersion(ctx)
	if err != nil {
		return false, nil
	}

	if runningVer.Equivalent(onDiskVer) {
		return false, nil
	}

	fmt.Printf("Running %s is at %s; on-disk binary is %s. Restarting to pick up the new binary.\n",
		serviceName, runningVer.Display(), onDiskVer.Display())

	if err := mgr.RestartAndVerify(ctx); err != nil {
		return true, err
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
		fmt.Printf("  Old: %s\n", oldVersion.Display())
		fmt.Printf("  New: %s\n", newVersion.Display())
	}
}
