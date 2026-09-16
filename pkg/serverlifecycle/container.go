package serverlifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"

	"miren.dev/runtime/pkg/serverinfo"
)

// ContainerRestarter restarts a container install by ending the process.
// There is no supervisor inside the container to ask; the one outside it
// (the container runtime's restart policy) starts a new container when this
// one exits, and container-boot in the image execs whatever the volume's
// release directory holds by then.
type ContainerRestarter struct {
	// Shutdown asks this process to stop. The default sends SIGTERM to
	// itself, which the server handles the same way as one from outside: the
	// boot graph's stop path takes the nested stack down cleanly.
	Shutdown func() error
	// ExitAfter bounds the graceful stop: a restart that never finishes
	// stopping would leave the container running the build it was meant to
	// replace, with no supervisor to notice. 0 means DefaultExitAfter.
	ExitAfter time.Duration
}

// DefaultExitAfter is past the server's own shutdown timeout, so it only
// ever fires on a stop that is stuck rather than slow.
const DefaultExitAfter = 8 * time.Minute

func (r ContainerRestarter) Restart(context.Context) error {
	exitAfter := r.ExitAfter
	if exitAfter <= 0 {
		exitAfter = DefaultExitAfter
	}
	timer := time.AfterFunc(exitAfter, func() {
		fmt.Fprintf(os.Stderr, "server still running %s after a restart was requested; exiting so the container restarts\n", exitAfter)
		os.Exit(1)
	})
	var err error
	if r.Shutdown != nil {
		err = r.Shutdown()
	} else {
		err = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}
	if err != nil {
		// No restart is underway, and the executor records the failure; a
		// forced exit on top would be a restart nobody asked for.
		timer.Stop()
	}
	return err
}

// ContainerLauncher runs the executor inside the server process. Nothing
// outlives the server in a container, so the executor dies with it at the
// restart it asked for. That is fine: the operation record in the volume is
// the checkpoint, and the next instance's Resume picks it up where it was,
// verifying itself as the new instance.
type ContainerLauncher struct {
	// NewExecutor builds an executor over the store; it is called once per
	// launch or resume so each run has its own downloaded-artifact state.
	NewExecutor func() (*Executor, error)
	Log         *slog.Logger

	// Runs are bound to ctx, not the launch's: the launch context is cut
	// loose from an RPC and times out in seconds, while a run lasts until
	// the server it is inside goes down.
	ctx context.Context
	wg  sync.WaitGroup
}

// NewContainerLauncher binds runs to ctx. Wait returns once every run
// launched under it has returned.
func NewContainerLauncher(ctx context.Context, log *slog.Logger, newExecutor func() (*Executor, error)) *ContainerLauncher {
	return &ContainerLauncher{NewExecutor: newExecutor, Log: log, ctx: ctx}
}

func (l *ContainerLauncher) Launch(_ context.Context, opID string) error {
	ex, err := l.NewExecutor()
	if err != nil {
		return fmt.Errorf("build executor: %w", err)
	}
	l.wg.Go(func() {
		op, err := ex.Run(l.ctx, opID)
		switch {
		case err != nil && l.ctx.Err() != nil:
			// The server is going down, quite possibly because this run
			// asked it to. The record keeps the phase for the next instance.
			l.Log.Info("lifecycle operation paused with the server", "operation", opID)
		case err != nil:
			l.Log.Error("lifecycle operation could not run", "operation", opID, "error", err)
		default:
			l.Log.Info("lifecycle operation finished", "operation", opID, "action", op.Action, "phase", op.Phase)
		}
	})
	return nil
}

// Resume relaunches the operation the previous instance left unfinished, if
// there is one. It answers the question a systemd install never has to ask:
// the transient unit there outlives the restart, but here the executor was
// the process that just exited.
func (l *ContainerLauncher) Resume(ctx context.Context, store *Store) error {
	op, err := store.Active()
	if err != nil {
		return fmt.Errorf("look for an unfinished operation: %w", err)
	}
	if op == nil {
		return nil
	}
	l.Log.Info("resuming lifecycle operation from the previous instance", "operation", op.ID, "action", op.Action, "phase", op.Phase)
	return l.Launch(ctx, op.ID)
}

// Wait blocks until every launched run has returned. Runs return promptly
// once the launcher's context is cancelled.
func (l *ContainerLauncher) Wait() {
	l.wg.Wait()
}

// InstanceProber reads this process's own serverinfo instead of asking the
// health endpoint. Inside the container the executor and the server are the
// same process, so there is nothing to reach over the network, and the
// answer is available before ingress is listening.
type InstanceProber struct {
	Source *serverinfo.Source
}

func (p InstanceProber) Probe(context.Context) (Snapshot, error) {
	info := p.Source.Info()
	return Snapshot{
		InstanceID:  info.InstanceID,
		Version:     info.Version,
		Commit:      info.Commit,
		Ready:       info.Ready,
		InstallKind: string(info.InstallKind),
	}, nil
}
