package commands

import (
	"fmt"

	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/serverlifecycle"
	"miren.dev/runtime/pkg/ui"
)

// The cluster verbs act on a server the CLI reaches over RPC, unlike
// `miren server restart` and `miren upgrade`, which act on this host. Keeping
// them apart is deliberate: a forgotten sudo must not turn into a restart of
// whichever cluster happens to be active.

func ClusterRestart(ctx *Context, opts struct {
	ConfigCentric
	Yes bool `long:"yes" short:"y" description:"Skip the confirmation prompt"`
}) error {
	if err := confirmClusterOperation(ctx, opts.Yes, "Restart"); err != nil {
		return err
	}
	op, err := startRemoteOperation(ctx, serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli"))
	if err != nil {
		return err
	}
	if !op.Succeeded() {
		return fmt.Errorf("restart %s: %s", op.Phase, op.Error)
	}
	ctx.Completed("Server restarted (%s)", op.NewVersion)
	return nil
}

func ClusterUpgrade(ctx *Context, opts struct {
	ConfigCentric
	Version        string `short:"V" long:"version" description:"Specific version to upgrade to (e.g., v0.2.0)"`
	Channel        string `long:"channel" description:"Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)"`
	Release        bool   `short:"r" long:"release" description:"Upgrade full release package (not just base)"`
	NoAutoRollback bool   `long:"no-auto-rollback" description:"Disable automatic rollback on failure"`
	HealthTimeout  int    `long:"health-timeout" default:"0" description:"Seconds to wait for the restarted server to report ready"`
	Yes            bool   `long:"yes" short:"y" description:"Skip the confirmation prompt"`
}) error {
	version, err := resolveVersionChannel(opts.Version, opts.Channel)
	if err != nil {
		return err
	}
	if err := confirmClusterOperation(ctx, opts.Yes, "Upgrade to "+version+" on"); err != nil {
		return err
	}
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "cli")
	op.TargetVersion = version
	if opts.Release {
		op.ArtifactType = string(release.ArtifactTypeRelease)
	}
	op.NoRollback = opts.NoAutoRollback
	op.ReadyTimeoutSeconds = opts.HealthTimeout
	result, err := startRemoteOperation(ctx, op)
	if err != nil {
		return err
	}
	if !result.Succeeded() {
		return fmt.Errorf("upgrade %s: %s", result.Phase, result.Error)
	}
	ctx.Completed("Server upgraded: %s", operationVersions(result))
	return nil
}

// confirmClusterOperation names the target before anything is sent. The
// cluster name comes from -C or the active configuration; with neither, the
// CLI is talking to its default server address and says so.
func confirmClusterOperation(ctx *Context, yes bool, verb string) error {
	target := ctx.ClusterName
	if target == "" {
		target = "the server at " + ctx.Config.ServerAddress
	} else {
		target = "cluster " + target
	}
	if yes {
		ctx.Info("%s %s", verb, target)
		return nil
	}
	confirmed, err := ui.Confirm(
		ui.WithMessage(fmt.Sprintf("%s %s? The server restarts and is unavailable until it comes back.", verb, target)),
		ui.WithDefault(false),
	)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("cancelled")
	}
	return nil
}
