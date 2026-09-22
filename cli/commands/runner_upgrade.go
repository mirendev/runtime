//go:build linux

package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// RunnerUpgrade is kept for existing scripts; it runs the same operation
// `miren upgrade` does on a runner host, with a nudge.
func RunnerUpgrade(ctx *Context, opts struct {
	Version        string `short:"V" long:"version" description:"Specific version to upgrade to (e.g., v0.2.0); default is the coordinator's build"`
	Channel        string `long:"channel" description:"Channel to use instead of matching the coordinator: 'latest' (stable releases) or 'main' (bleeding edge)"`
	Check          bool   `short:"c" long:"check" description:"Check for available updates only"`
	Force          bool   `short:"f" long:"force" description:"Force upgrade even if already up to date"`
	SkipHealth     bool   `long:"skip-health" description:"Deprecated: readiness is always verified"`
	NoAutoRollback bool   `long:"no-auto-rollback" description:"Disable automatic rollback on failure"`
	HealthTimeout  int    `long:"health-timeout" default:"0" description:"Seconds to wait for the restarted runner to report ready"`
}) error {
	ctx.Warn("'miren runner upgrade' is deprecated; 'sudo miren upgrade' now upgrades the runner and the CLI together.")

	if os.Geteuid() != 0 {
		return fmt.Errorf("runner upgrade requires root privileges (use sudo)")
	}
	if !release.IsRunnerRunning() {
		return fmt.Errorf("miren runner is not running. Use 'miren upgrade' to upgrade the CLI binary instead")
	}
	version, exact, err := daemonTarget(ctx, runnerDaemon, opts.Version, opts.Channel)
	if err != nil {
		return err
	}
	if opts.Check {
		return checkDaemonUpgrade(ctx, runnerDaemon, version, exact)
	}
	if opts.SkipHealth {
		ctx.Warn("--skip-health is ignored: the upgrade verifies the new runner reports ready before finishing.")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	customize := func(op *serverlifecycle.Operation) {
		op.NoRollback = opts.NoAutoRollback
		op.ReadyTimeoutSeconds = opts.HealthTimeout
	}
	return upgradeDaemonAndCLI(ctx, runnerDaemon, version, exact, exe, opts.Force, customize)
}

// RunnerUpgradeRollback rolls back the runner to the previous version
func RunnerUpgradeRollback(ctx *Context, opts struct {
	SkipHealth bool `long:"skip-health" description:"Skip health check after rollback"`
}) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("runner rollback requires root privileges (use sudo)")
	}

	mgrOpts := release.RunnerManagerOptions()
	mgrOpts.SkipHealthCheck = opts.SkipHealth
	mgr := release.NewManager(mgrOpts)

	fmt.Println("Rolling back runner to previous version...")
	if err := mgr.Rollback(ctx); err != nil {
		return err
	}

	fmt.Println("\nRunner rollback successful!")
	return nil
}
