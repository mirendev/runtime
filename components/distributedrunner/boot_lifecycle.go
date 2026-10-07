//go:build linux

package distributedrunner

import (
	"context"
	"log/slog"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverinfo"
	"miren.dev/runtime/pkg/serverlifecycle"
	serverinfosrv "miren.dev/runtime/servers/serverinfo"
	lifecyclesrv "miren.dev/runtime/servers/serverlifecycle"
)

const (
	// ServerInfoService is the same name the server uses: it describes the
	// process answering, whichever daemon that is.
	ServerInfoService = "dev.miren.runtime/server-info"
	// RunnerLifecycleService serves the runner's restart and upgrade ledger.
	// Mirrored in cli/commands, which cannot import this linux-only package.
	RunnerLifecycleService = "dev.miren.runtime/runner-lifecycle"
)

type lifecycleBoot struct {
	component *boot.Component
	log       *slog.Logger
	instance  *serverinfo.Source
}

// Exposed as soon as the RPC server is up, before the sandbox host and node
// presence, so an executor restarting this runner can watch the new process
// become ready and the coordinator can reach the ledger while the node is
// still coming up.
func newLifecycleBoot(log *slog.Logger, instance *serverinfo.Source, clusterAccess boot.Output[clusterAccessBootOutput]) *lifecycleBoot {
	b := &lifecycleBoot{log: log, instance: instance}
	b.component = boot.Run1("lifecycle", clusterAccess, b.start)
	return b
}

func (b *lifecycleBoot) start(_ context.Context, clusterAccess clusterAccessBootOutput) error {
	store, err := serverlifecycle.NewStore(serverlifecycle.RunnerDir)
	if err != nil {
		return err
	}
	exe, err := serverlifecycle.ExecutorBinary()
	if err != nil {
		return err
	}
	launcher := serverlifecycle.SystemdLauncher{Binary: exe, Command: serverlifecycle.RunnerExecutorCommand}

	server := clusterAccess.access.Server()
	server.ExposeValue(ServerInfoService, server_v1alpha.AdaptServerInfo(serverinfosrv.NewServer(b.instance)))
	server.ExposeValue(RunnerLifecycleService, server_v1alpha.AdaptServerLifecycle(
		lifecyclesrv.NewServer(store, launcher, b.log.With("component", "runner-lifecycle")).ForRunner()))
	b.log.Info("runner lifecycle ready", "dir", serverlifecycle.RunnerDir, "executor", exe)
	return nil
}
