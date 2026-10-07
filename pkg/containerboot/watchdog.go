package containerboot

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"miren.dev/runtime/pkg/serverlifecycle"
)

// Watchdog is the one observer of an upgraded build that hangs before the
// executor inside it can run. A build that crashes is caught by the boot
// guard's attempt count, and one that boots far enough to run the executor
// is caught by that executor's own ready deadline; a hang in between has
// nothing watching it, since the boot graph has no overall timeout. Under
// systemd the executor is an external process probing the health endpoint
// against a deadline, and this is that deadline for a container.
//
// container-boot starts one beside the server for each boot of the new
// build. It watches the ledger, not the server: the operation leaving the
// phases that mean "the new build is coming up" is the executor having
// taken over, whatever the outcome. Past the deadline with the operation
// still there, it rolls back the same way the boot guard does and ends the
// server so the restart policy brings the previous build up.
type Watchdog struct {
	Boot
	OperationID string
	// ServerPID is the process to end on rollback, container-boot's own
	// PID, which the exec keeps.
	ServerPID int
	// Timeout is how long the build gets; 0 means the operation's own ready
	// timeout, or the executor's default.
	Timeout  time.Duration
	Interval time.Duration
	// Kill ends the server; nil means the signal sequence in killServer.
	Kill func(pid int) error
}

// ErrWatchedOperationGone is returned when the operation the watchdog was
// started for is not in the ledger.
var ErrWatchedOperationGone = errors.New("watched operation is not in the ledger")

// Run returns once the operation has moved on, the server has exited, or the
// deadline passed and the rollback was done. Only an error in the ledger or
// the rollback itself is an error.
func (w Watchdog) Run(ctx context.Context) error {
	store, err := serverlifecycle.NewStore(w.LifecycleDir)
	if err != nil {
		return err
	}
	op, err := store.Get(w.OperationID)
	if err != nil {
		if errors.Is(err, serverlifecycle.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrWatchedOperationGone, w.OperationID)
		}
		return err
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = serverlifecycle.DefaultOptions().ReadyTimeout
		if op.ReadyTimeoutSeconds > 0 {
			timeout = time.Duration(op.ReadyTimeoutSeconds) * time.Second
		}
	}
	interval := w.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	installer := w.installer()
	w.Log.Info("watching the upgraded build", "operation", op.ID, "version", op.ResolvedVersion, "timeout", timeout)

	for {
		op, err = store.Get(w.OperationID)
		if err != nil {
			if errors.Is(err, serverlifecycle.ErrNotFound) {
				return fmt.Errorf("%w: %s", ErrWatchedOperationGone, w.OperationID)
			}
			// A record mid-rewrite; the next read will do.
			w.Log.Warn("could not read the watched operation", "operation", w.OperationID, "error", err)
		} else if !w.bootsNewBuild(ctx, op, installer) {
			w.Log.Info("upgrade moved on; nothing left to watch", "operation", op.ID, "phase", op.Phase)
			return nil
		}
		if !processAlive(w.ServerPID) {
			w.Log.Info("server exited; the next boot decides", "operation", w.OperationID)
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	// The executor inside the server holds the operation's lock while it
	// runs. If it does, the build got far enough to run it, and its own
	// deadline is the one that counts.
	unlock, err := store.LockOperation(w.OperationID)
	if errors.Is(err, serverlifecycle.ErrLocked) {
		w.Log.Info("the executor is running inside the server and owns the upgrade; leaving it to its own deadline", "operation", w.OperationID)
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	op, err = store.Get(w.OperationID)
	if err != nil {
		return err
	}
	if !w.bootsNewBuild(ctx, op, installer) {
		return nil
	}
	if err := w.rollBack(ctx, store, op, installer, fmt.Sprintf("upgraded build %s did not report ready within %s", op.ResolvedVersion, timeout)); err != nil {
		return err
	}
	if op.Phase != serverlifecycle.PhaseRollingBack {
		// Nothing to roll back to, so the record is failed and the hung
		// build stays; ending it would only start it again.
		return nil
	}
	w.Log.Warn("ending the hung server so the previous build can come up", "operation", op.ID, "pid", w.ServerPID)
	if w.Kill != nil {
		return w.Kill(w.ServerPID)
	}
	return killServer(w.ServerPID)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// killServer asks nicely, then insists. The server's signal handler treats
// a second SIGTERM as urgency and exits at once; SIGKILL is for a process
// too far gone to run the handler.
func killServer(pid int) error {
	for _, step := range []struct {
		sig  syscall.Signal
		wait time.Duration
	}{
		{syscall.SIGTERM, 30 * time.Second},
		{syscall.SIGTERM, 10 * time.Second},
		{syscall.SIGKILL, 5 * time.Second},
	} {
		if err := syscall.Kill(pid, step.sig); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return nil
			}
			return fmt.Errorf("signal %s to %d: %w", step.sig, pid, err)
		}
		end := time.Now().Add(step.wait)
		for time.Now().Before(end) {
			if !processAlive(pid) {
				return nil
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	return fmt.Errorf("server %d is still running after SIGKILL", pid)
}
