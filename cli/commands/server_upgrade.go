package commands

import (
	"fmt"
	"os"
	"time"

	"miren.dev/runtime/pkg/release"
)

// ServerUpgrade upgrades the miren server to the latest or specified version
func ServerUpgrade(ctx *Context, opts struct {
	Version        string `short:"v" long:"version" description:"Specific version to upgrade to (default: main)"`
	Check          bool   `short:"c" long:"check" description:"Check for available updates only"`
	Force          bool   `short:"f" long:"force" description:"Force upgrade even if already up to date"`
	Release        bool   `short:"r" long:"release" description:"Upgrade full release package (not just base)"`
	SkipHealth     bool   `long:"skip-health" description:"Skip health check after upgrade"`
	NoAutoRollback bool   `long:"no-auto-rollback" description:"Disable automatic rollback on failure"`
	HealthTimeout  int    `long:"health-timeout" description:"Health check timeout in seconds (default: 60)"`
}) error {
	// Check if running with sufficient privileges
	if os.Geteuid() != 0 {
		return fmt.Errorf("server upgrade requires root privileges (use sudo)")
	}

	// Check if server is actually running
	if !release.IsServerRunning() {
		return fmt.Errorf("miren server is not running. Use 'miren upgrade' to upgrade the CLI binary instead")
	}

	// Determine target version
	version := opts.Version
	if version == "" {
		version = "main" // Default to main branch
	}

	// Create manager with server configuration
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.SkipHealthCheck = opts.SkipHealth
	mgrOpts.AutoRollback = !opts.NoAutoRollback
	if opts.HealthTimeout > 0 {
		mgrOpts.HealthTimeout = time.Duration(opts.HealthTimeout) * time.Second
	}

	// If just checking for updates
	if opts.Check {
		// For server check, use default server path (nil passes through to default)
		current, latest, err := CheckVersionStatus(ctx, version, nil)
		if err != nil {
			return err
		}

		PrintVersionComparison(current, latest)

		// Use proper version comparison with build dates
		if latest.IsNewer(current) {
			fmt.Println("\nAn update is available! Run 'sudo miren server upgrade' to install it.")
		} else {
			fmt.Println("\nYour server is already on the latest version.")
		}
		return nil
	}

	// Check if upgrade is needed (unless forced)
	// For server upgrade, use default server path (nil passes through to default)
	needsUpgrade, err := CheckIfUpgradeNeeded(ctx, version, opts.Force, nil)
	if err != nil {
		ctx.Log.Warn("could not check version status", "error", err)
		// Continue with upgrade if we can't check
	} else if !needsUpgrade {
		return nil // Already up to date
	}

	mgr := release.NewManager(mgrOpts)

	// Get current version for comparison after upgrade
	current, err := mgr.GetCurrentVersion(ctx)
	if err != nil {
		ctx.Log.Warn("could not determine current version", "error", err)
	}

	// Determine artifact type
	artifactType := release.ArtifactTypeBase
	if opts.Release {
		artifactType = release.ArtifactTypeRelease
	}

	// Create artifact descriptor
	artifact := release.NewArtifact(artifactType, version)

	// Perform server upgrade (includes restart and health check)
	fmt.Printf("Upgrading server to %s version %s...\n", artifactType, version)
	if err := mgr.UpgradeServer(ctx, artifact); err != nil {
		return err
	}

	// Report final status
	// For server upgrade, use default server path (nil passes through to default)
	PrintUpgradeSuccess(ctx, current, "Server", nil)

	return nil
}

// ServerUpgradeRollback rolls back the server to the previous version
func ServerUpgradeRollback(ctx *Context, opts struct {
	SkipHealth bool `long:"skip-health" description:"Skip health check after rollback"`
}) error {
	// Check if running with sufficient privileges
	if os.Geteuid() != 0 {
		return fmt.Errorf("server rollback requires root privileges (use sudo)")
	}

	// Create manager
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.SkipHealthCheck = opts.SkipHealth
	mgr := release.NewManager(mgrOpts)

	// Perform rollback
	fmt.Println("Rolling back server to previous version...")
	if err := mgr.Rollback(ctx); err != nil {
		return err
	}

	fmt.Println("\nServer rollback successful!")
	return nil
}
