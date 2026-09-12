package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// ServerUpgrade is kept for existing scripts; it runs the same operation
// `miren upgrade` does, with a nudge.
func ServerUpgrade(ctx *Context, opts struct {
	Version        string `short:"V" long:"version" description:"Specific version to upgrade to (e.g., v0.2.0)"`
	Channel        string `long:"channel" description:"Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)"`
	Check          bool   `short:"c" long:"check" description:"Check for available updates only"`
	Force          bool   `short:"f" long:"force" description:"Force upgrade even if already up to date"`
	Release        bool   `short:"r" long:"release" description:"Upgrade full release package (not just base)"`
	SkipHealth     bool   `long:"skip-health" description:"Deprecated: readiness is always verified"`
	NoAutoRollback bool   `long:"no-auto-rollback" description:"Disable automatic rollback on failure"`
	HealthTimeout  int    `long:"health-timeout" default:"0" description:"Seconds to wait for the restarted server to report ready"`
}) error {
	ctx.Warn("'miren server upgrade' is deprecated; 'sudo miren upgrade' now upgrades the server and the CLI together.")

	if os.Geteuid() != 0 {
		return fmt.Errorf("server upgrade requires root privileges (use sudo)")
	}
	if !release.IsServerRunning() {
		return fmt.Errorf("miren server is not running. Use 'miren upgrade' to upgrade the CLI binary instead")
	}
	version, err := resolveVersionChannel(opts.Version, opts.Channel)
	if err != nil {
		return err
	}
	serverOpts := release.DefaultManagerOptions()
	if opts.Check {
		return checkServerUpgrade(ctx, version, serverOpts)
	}
	if opts.SkipHealth {
		ctx.Warn("--skip-health is ignored: the upgrade verifies the new server reports ready before finishing.")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	customize := func(op *serverlifecycle.Operation) {
		if opts.Release {
			op.ArtifactType = string(release.ArtifactTypeRelease)
		}
		op.NoRollback = opts.NoAutoRollback
		op.ReadyTimeoutSeconds = opts.HealthTimeout
	}
	return upgradeServerAndCLI(ctx, version, exe, opts.Force, serverOpts, customize)
}

// ServerUpgradeRollback rolls back the server to the previous version
func ServerUpgradeRollback(ctx *Context, opts struct {
	SkipHealth bool `long:"skip-health" description:"Skip health check after rollback"`
}) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("server rollback requires root privileges (use sudo)")
	}

	mgrOpts := release.DefaultManagerOptions()
	mgrOpts.SkipHealthCheck = opts.SkipHealth
	mgr := release.NewManager(mgrOpts)

	fmt.Println("Rolling back server to previous version...")
	if err := mgr.Rollback(ctx); err != nil {
		return err
	}

	fmt.Println("\nServer rollback successful!")
	return nil
}
