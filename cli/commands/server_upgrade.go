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

	// Create manager with server configuration
	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.SkipHealthCheck = opts.SkipHealth
	mgrOpts.AutoRollback = !opts.NoAutoRollback
	if opts.HealthTimeout > 0 {
		mgrOpts.HealthTimeout = time.Duration(opts.HealthTimeout) * time.Second
	}

	mgr := release.NewManager(mgrOpts)

	// Check current version
	current, err := mgr.GetCurrentVersion(ctx)
	if err != nil {
		ctx.Log.Warn("could not determine current version", "error", err)
	}

	// Determine target version
	version := opts.Version
	if version == "" {
		version = "main" // Default to main branch
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
	newVersion, _ := mgr.GetCurrentVersion(ctx)
	if current.Version != "" {
		fmt.Printf("\nServer upgrade successful:\n")
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
	}

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
