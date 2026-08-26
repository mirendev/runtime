//go:build linux

package commands

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
	servercomponent "miren.dev/runtime/components/server"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/version"
)

func Server(ctx *Context, opts serverconfig.CLIFlags) error {
	eg, sub := errgroup.WithContext(ctx)

	// Load configuration from all sources with precedence:
	// CLI flags > Environment variables > Config file > Defaults
	configFile := ""
	if opts.ConfigFile != nil {
		configFile = *opts.ConfigFile
	}
	cfg, err := serverconfig.Load(configFile, &opts, ctx.Log)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := cfg.ValidateIngressCoherence(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	if err := cfg.ValidateMetricsCoherence(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	cfg.WarnDeprecatedConfig(ctx.Log)

	// Initialize Miren Labs feature flags
	labs.Init(ctx.Log, cfg.Labs)

	versionInfo := version.GetInfo()
	ctx.UILog.Info("starting miren server", "version", versionInfo.Version, "commit", versionInfo.Commit)

	if err := prepareServerConfig(ctx, cfg); err != nil {
		return err
	}

	klog.SetLogger(logr.FromSlogHandler(ctx.Log.With("module", "global").Handler()))

	runtime, err := servercomponent.Start(servercomponent.StartOptions{
		Log:     ctx.Log,
		Context: sub,
		Group:   eg,
		Config:  cfg,
	})
	if err != nil {
		ctx.Log.Error("failed to start runtime dependency graph", "error", err)
		return err
	}
	ctx.Log = runtime.Log()
	klog.SetLogger(logr.FromSlogHandler(ctx.Log.With("module", "global").Handler()))
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), servercomponent.ShutdownTimeout)
		defer cancel()
		if err := runtime.Stop(stopCtx); err != nil {
			ctx.Log.Error("failed to stop runtime dependency graph", "error", err)
		}
	}()

	if err := configureServerClient(ctx, cfg, runtime.Coordinator); err != nil {
		return err
	}

	ctx.UILog.Info("Miren server started", "address", cfg.Server.GetAddress(), "etcd_endpoints", cfg.Etcd.Endpoints, "etcd_prefix", cfg.Etcd.GetPrefix(), "runner_id", cfg.Server.GetRunnerID())

	ctx.Info("Miren server started successfully! You can now connect to the cluster using `-C %s`\n", cfg.Server.GetConfigClusterName())
	ctx.Info("For example: cd my-app && miren deploy -C %s", cfg.Server.GetConfigClusterName())

	eg.Go(func() error {
		return watchServerSignals(ctx, sub, runtime.Runner)
	})

	// Wait for all goroutines to complete or context to be cancelled
	err = eg.Wait()
	if err != nil && err != context.Canceled {
		ctx.Log.Error("error during execution", "error", err)
	}

	ctx.Log.Info("miren server shutting down, cleaning up resources")

	return err
}
