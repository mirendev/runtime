package coordinate

import (
	"context"
	"sync"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/lifecyclesync"
	"miren.dev/runtime/pkg/serverinfo"
	"miren.dev/runtime/pkg/serverlifecycle"
	lifecyclesrv "miren.dev/runtime/servers/serverlifecycle"
)

// ServerLifecycleService is the RPC service name for ServerLifecycle. Mirrored
// in cli/commands, which cannot import this linux-only package.
const ServerLifecycleService = "dev.miren.runtime/server-lifecycle"

// NewServerLifecycle serves the restart and upgrade ledger over RPC and to
// cloud. dir is the ledger directory; the executor that
// writes it runs outside this process.
func NewServerLifecycle(foundation *Foundation, instance *serverinfo.Source, dir string) *ServerLifecycle {
	return &ServerLifecycle{Foundation: foundation, instance: instance, dir: dir}
}

// ServerLifecycle owns the read side of pkg/serverlifecycle within the server.
// Writes still go through the file store; every reader here follows it.
type ServerLifecycle struct {
	*Foundation
	instance *serverinfo.Source
	dir      string

	store    *serverlifecycle.Store
	watcher  *lifecyclesync.Watcher
	reporter *lifecyclesync.Reporter

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (c *ServerLifecycle) Start(ctx context.Context) error {
	log := c.Log.With("component", "server-lifecycle")
	store, err := serverlifecycle.NewStore(c.dir)
	if err != nil {
		return err
	}
	exe, err := serverlifecycle.ExecutorBinary()
	if err != nil {
		return err
	}
	launcher := serverlifecycle.SystemdLauncher{Binary: exe}

	c.store = store
	c.watcher = lifecyclesync.NewWatcher(log, store)
	c.reporter = lifecyclesync.NewReporter(log, store, launcher, c.watcher, instanceIdentity{c.instance})

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.wg.Go(func() {
		if err := c.watcher.Run(runCtx); err != nil && runCtx.Err() == nil {
			log.Error("lifecycle watcher stopped", "error", err)
		}
	})

	c.Server().ExposeValue(ServerLifecycleService, server_v1alpha.AdaptServerLifecycle(
		lifecyclesrv.NewServer(store, kickingLauncher{launcher, c.watcher}, log)))
	log.Info("server lifecycle ready", "dir", c.dir, "executor", exe)
	return nil
}

func (c *ServerLifecycle) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

// Register adds the cloud capability to link.
func (c *ServerLifecycle) Register(ctx context.Context, link lifecyclesync.Link) error {
	return c.reporter.Register(ctx, link)
}

// instanceIdentity adapts serverinfo.Source to what the reporter announces.
type instanceIdentity struct{ source *serverinfo.Source }

func (i instanceIdentity) InstanceID() string {
	if i.source == nil {
		return ""
	}
	return i.source.InstanceID()
}

func (i instanceIdentity) InstallKind() string {
	if i.source == nil {
		return ""
	}
	return string(i.source.Info().InstallKind)
}

// kickingLauncher wakes the watcher after a launch so a record started over
// RPC is reported without waiting for the directory watch.
type kickingLauncher struct {
	serverlifecycle.Launcher
	watcher *lifecyclesync.Watcher
}

func (l kickingLauncher) Launch(ctx context.Context, opID string) error {
	err := l.Launcher.Launch(ctx, opID)
	l.watcher.Kick()
	return err
}

// Launched forwards the optional check so wrapping does not hide it from
// serverlifecycle.Start.
func (l kickingLauncher) Launched(ctx context.Context, opID string) bool {
	if checker, ok := l.Launcher.(serverlifecycle.LaunchChecker); ok {
		return checker.Launched(ctx, opID)
	}
	return false
}
