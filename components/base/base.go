// Package base provides a shared framework for managing containerd-based service components.
// Components like etcd, victorialogs, and victoriametrics embed BaseComponent to share
// common functionality for container lifecycle, exit monitoring, and auto-restart.
package base

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"golang.org/x/sys/unix"
)

// RestartPolicy configures the auto-restart behavior for a component.
type RestartPolicy struct {
	MaxRestarts int           // Maximum restart attempts before giving up (0 = unlimited)
	BackoffBase time.Duration // Base delay for exponential backoff (default: 2s)
	BackoffMax  time.Duration // Maximum backoff delay (default: 60s)
	ResetWindow time.Duration // Time after which restart count resets (default: 5m)
}

// DefaultRestartPolicy returns the default restart policy with unlimited restarts.
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		MaxRestarts: 0, // Never give up
		BackoffBase: 2 * time.Second,
		BackoffMax:  60 * time.Second,
		ResetWindow: 5 * time.Minute,
	}
}

// AggressiveRestartPolicy returns a restart policy with shorter backoff times,
// suitable for critical components like etcd that the system cannot run without.
func AggressiveRestartPolicy() RestartPolicy {
	return RestartPolicy{
		MaxRestarts: 0, // Never give up
		BackoffBase: 500 * time.Millisecond,
		BackoffMax:  10 * time.Second,
		ResetWindow: 2 * time.Minute,
	}
}

// ReadyCheckConfig configures the readiness check behavior.
type ReadyCheckConfig struct {
	MaxAttempts int           // Maximum number of attempts (default: 30)
	DialTimeout time.Duration // Timeout for each dial attempt (default: 2s)
	Interval    time.Duration // Time between attempts (default: 2s)
}

// DefaultReadyCheckConfig returns the default readiness check configuration.
func DefaultReadyCheckConfig() ReadyCheckConfig {
	return ReadyCheckConfig{
		MaxAttempts: 30,
		DialTimeout: 2 * time.Second,
		Interval:    2 * time.Second,
	}
}

// TaskCreator is a callback that creates a new containerd task for the component.
// Each component implements this to provide component-specific task creation (e.g., logging options).
type TaskCreator func(ctx context.Context, container containerd.Container) (containerd.Task, error)

// ReadyPortGetter is a callback that returns the port to check for readiness.
type ReadyPortGetter func() int

// BaseComponent provides shared functionality for containerd-based service components.
type BaseComponent struct {
	Log       *slog.Logger
	CC        *containerd.Client
	Namespace string
	DataPath  string

	// ComponentName is used in log messages to identify the component
	ComponentName string

	// Callbacks for component-specific behavior
	CreateTask    TaskCreator
	GetReadyPort  ReadyPortGetter
	RestartPolicy RestartPolicy
	ReadyConfig   ReadyCheckConfig

	// opMu serializes major operations like Start and Stop
	opMu sync.Mutex

	// stateMu protects state fields (container, task, running, etc.)
	stateMu       sync.Mutex
	container     containerd.Container
	task          containerd.Task
	running       bool
	stopMonitor   chan struct{}
	monitorDone   chan struct{}
	restartCount  int
	lastRestartAt time.Time
}

// NewBaseComponent creates a new BaseComponent with default policies.
func NewBaseComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath, componentName string) *BaseComponent {
	return &BaseComponent{
		Log:           log,
		CC:            cc,
		Namespace:     namespace,
		DataPath:      dataPath,
		ComponentName: componentName,
		RestartPolicy: DefaultRestartPolicy(),
		ReadyConfig:   DefaultReadyCheckConfig(),
	}
}

// LockOp acquires the operation mutex for serializing major operations.
func (b *BaseComponent) LockOp() {
	b.opMu.Lock()
}

// UnlockOp releases the operation mutex.
func (b *BaseComponent) UnlockOp() {
	b.opMu.Unlock()
}

// SetContainer sets the container reference.
func (b *BaseComponent) SetContainer(container containerd.Container) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	b.container = container
}

// GetContainer returns the current container reference.
func (b *BaseComponent) GetContainer() containerd.Container {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.container
}

// SetTask sets the task reference and marks the component as running.
func (b *BaseComponent) SetTask(task containerd.Task) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	b.task = task
	b.running = true
}

// GetTask returns the current task reference.
func (b *BaseComponent) GetTask() containerd.Task {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.task
}

// SetRunning sets the running state.
func (b *BaseComponent) SetRunning(running bool) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	b.running = running
}

// IsRunning returns whether the component is currently running.
func (b *BaseComponent) IsRunning() bool {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.running
}

// IfRunning executes the given function while holding the state lock, but only if
// the component is currently running. Returns empty string if not running.
// This is useful for safely reading component state that should only be accessed
// when the component is running.
func (b *BaseComponent) IfRunning(fn func() string) string {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	if !b.running {
		return ""
	}
	return fn()
}

// Stop stops the component, including the exit monitor, task, and container.
// This method acquires the operation mutex.
func (b *BaseComponent) Stop(ctx context.Context) error {
	b.opMu.Lock()
	defer b.opMu.Unlock()

	return b.stopInternal(ctx)
}

// stopInternal performs the stop operation. Caller must hold opMu.
func (b *BaseComponent) stopInternal(ctx context.Context) error {
	b.stateMu.Lock()
	if !b.running {
		b.stateMu.Unlock()
		return nil
	}

	// Stop the exit monitor first
	stopMonitor := b.stopMonitor
	monitorDone := b.monitorDone
	// Clear the channels before releasing lock so new StartExitMonitor can run
	b.stopMonitor = nil
	b.monitorDone = nil
	b.stateMu.Unlock()

	// Signal the monitor goroutine to stop (must be done after releasing lock)
	if stopMonitor != nil {
		close(stopMonitor)
	}

	// Wait for monitor to finish
	if monitorDone != nil {
		<-monitorDone
	}

	b.stateMu.Lock()
	task := b.task
	container := b.container
	b.stateMu.Unlock()

	ctx = namespaces.WithNamespace(ctx, b.Namespace)

	if task != nil {
		b.stopTask(ctx, task)
		b.stateMu.Lock()
		b.task = nil
		b.stateMu.Unlock()
	}

	if container != nil {
		b.deleteContainerWithRetry(ctx, container)
		b.stateMu.Lock()
		b.container = nil
		b.stateMu.Unlock()
	}

	b.stateMu.Lock()
	b.running = false
	b.stateMu.Unlock()

	b.Log.Info(b.ComponentName + " server stopped")

	return nil
}

// stopTask gracefully stops a task with SIGTERM, escalating to SIGKILL if needed.
func (b *BaseComponent) stopTask(ctx context.Context, task containerd.Task) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		// The init process may already be gone — e.g. the previous server died
		// uncleanly, leaving the task registered but its process dead, so SIGTERM
		// comes back "not found". Do NOT bail here: we still have to delete the
		// task record below, otherwise the container (and its snapshot) can't be
		// removed and the next plain→TLS recreate wedges on "snapshot already
		// exists". See MIR-1463.
		b.Log.Warn("failed to send SIGTERM to "+b.ComponentName+" task, proceeding to delete", "error", err)
	} else {
		status, err := task.Wait(shutdownCtx)
		if err == nil {
			select {
			case es := <-status:
				b.Log.Info(b.ComponentName+" task exited", "code", es.ExitCode())

			case <-shutdownCtx.Done():
				b.Log.Warn(b.ComponentName + " task did not exit gracefully, sending SIGKILL")
				killCtx, killCancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), b.Namespace), 10*time.Second)
				defer killCancel()

				if err := task.Kill(killCtx, unix.SIGKILL); err != nil {
					b.Log.Error("failed to send SIGKILL to "+b.ComponentName+" task", "error", err)
				} else {
					statusCh, waitErr := task.Wait(killCtx)
					if waitErr != nil {
						b.Log.Error(b.ComponentName+" task wait channel after SIGKILL failed", "error", waitErr)
					} else {
						select {
						case <-statusCh:
							// Task exited
						case <-killCtx.Done():
							b.Log.Warn(b.ComponentName + " task did not exit after SIGKILL within timeout")
						}
					}
				}
			}
		}
	}

	deleteCtx, deleteCancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), b.Namespace), 10*time.Second)
	defer deleteCancel()

	// WithProcessKill force-kills any lingering process before removing the task
	// record, so a task whose SIGTERM failed above is still cleaned up in one shot.
	// A not-found task is already gone — treat that as success.
	if _, err := task.Delete(deleteCtx, containerd.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
		b.Log.Error("failed to delete "+b.ComponentName+" task", "error", err)
	}
}

// StopTask is the exported version of stopTask for use by components.
func (b *BaseComponent) StopTask(ctx context.Context, task containerd.Task) {
	b.stopTask(ctx, task)
}

// deleteContainerAndSnapshot deletes the container and makes sure its snapshot is
// gone. It reads the snapshotter + snapshot key first, deletes the container with
// WithSnapshotCleanup, then explicitly removes the snapshot as a fallback so a failed
// or partial container delete can't strand it — a stranded snapshot wedges the next
// recreate on "snapshot already exists" (MIR-1463). A not-found container or snapshot
// is treated as success.
//
// It runs on a detached, namespaced context with its own deadline (like stopTask's
// final delete) so a cancelled caller — e.g. a shutdown — can't abort the delete or
// the snapshot removal partway and leak the very snapshot this path exists to reclaim.
func (b *BaseComponent) deleteContainerAndSnapshot(container containerd.Container) error {
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), b.Namespace), 30*time.Second)
	defer cancel()

	var snapshotterName, snapshotKey string
	if info, err := container.Info(ctx); err == nil {
		snapshotterName = info.Snapshotter
		snapshotKey = info.SnapshotKey
	}

	delErr := container.Delete(ctx, containerd.WithSnapshotCleanup)
	if errdefs.IsNotFound(delErr) {
		delErr = nil
	}

	// Fallback: ensure the snapshot is gone regardless of how the container delete
	// fared. Harmless if WithSnapshotCleanup already removed it (Remove tolerates a
	// missing key), and it's the backstop that keeps a failed delete from leaking.
	if snapshotterName != "" && snapshotKey != "" {
		if snapshotter := b.CC.SnapshotService(snapshotterName); snapshotter != nil {
			if err := snapshotter.Remove(ctx, snapshotKey); err != nil && !errdefs.IsNotFound(err) {
				b.Log.Warn("failed to explicitly remove "+b.ComponentName+" snapshot", "error", err, "snapshot_key", snapshotKey)
			}
		}
	}

	return delErr
}

// deleteContainerWithRetry deletes the container (and its snapshot) with retry logic.
func (b *BaseComponent) deleteContainerWithRetry(ctx context.Context, container containerd.Container) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// The delete itself runs on a detached context so it always finishes; ctx
		// only governs whether we keep retrying, so a shutdown stops the backoff
		// loop promptly instead of blocking on it.
		err := b.deleteContainerAndSnapshot(container)
		if err == nil {
			b.Log.Info(b.ComponentName + " container deleted successfully")
			return
		}
		b.Log.Error("failed to delete "+b.ComponentName+" container", "error", err, "attempt", attempt, "max_retries", maxRetries)

		if attempt < maxRetries {
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				b.Log.Warn(b.ComponentName+" container delete retries cancelled", "error", ctx.Err())
				return
			}
		}
	}

	b.Log.Error("failed to delete " + b.ComponentName + " container after all retries, potential snapshot leak")
}

// DeleteContainerWithRetry is the exported version for use by components.
func (b *BaseComponent) DeleteContainerWithRetry(ctx context.Context) {
	b.stateMu.Lock()
	container := b.container
	b.stateMu.Unlock()
	if container != nil {
		b.deleteContainerWithRetry(ctx, container)
	}
}

// CleanupExistingContainer stops the task and deletes a container during restart scenarios.
// It shares the same task-delete and snapshot-cleanup path as the graceful Stop flow, so
// a task left registered by an unclean shutdown still gets force-deleted and its snapshot
// removed rather than stranding both and wedging the recreate (MIR-1463).
func (b *BaseComponent) CleanupExistingContainer(ctx context.Context, container containerd.Container) {
	task, err := container.Task(ctx, nil)
	if err == nil {
		b.stopTask(ctx, task)
	}

	b.deleteContainerWithRetry(ctx, container)
}

// WaitForReady waits until the component is ready by checking TCP connectivity.
func (b *BaseComponent) WaitForReady(ctx context.Context, host string, port int) error {
	endpoint := net.JoinHostPort(host, strconv.Itoa(port))

	for i := 0; i < b.ReadyConfig.MaxAttempts; i++ {
		conn, err := net.DialTimeout("tcp", endpoint, b.ReadyConfig.DialTimeout)
		if err == nil {
			conn.Close()
			b.Log.Info(b.ComponentName+" server is ready", "endpoint", endpoint)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.ReadyConfig.Interval):
			continue
		}
	}

	b.Log.Warn(b.ComponentName+" server readiness check timed out", "endpoint", endpoint)
	return fmt.Errorf("%s readiness check timed out after %d attempts", b.ComponentName, b.ReadyConfig.MaxAttempts)
}

// StartExitMonitor starts a goroutine that monitors for unexpected task exits.
// If a monitor is already running, this is a no-op.
func (b *BaseComponent) StartExitMonitor(ctx context.Context) {
	b.stateMu.Lock()
	// Check if monitor is already running
	if b.stopMonitor != nil {
		b.stateMu.Unlock()
		b.Log.Debug(b.ComponentName + " exit monitor already running, skipping")
		return
	}
	b.stopMonitor = make(chan struct{})
	b.monitorDone = make(chan struct{})
	task := b.task
	if task == nil {
		b.stopMonitor = nil
		b.monitorDone = nil
		b.stateMu.Unlock()
		b.Log.Error(b.ComponentName + " cannot start exit monitor: task is nil")
		return
	}
	// Capture channels before releasing lock to avoid race with stopInternal
	stopMonitor := b.stopMonitor
	monitorDone := b.monitorDone
	b.stateMu.Unlock()

	ctx = namespaces.WithNamespace(ctx, b.Namespace)

	exitCh, err := task.Wait(ctx)
	if err != nil {
		b.Log.Error("failed to get exit channel for "+b.ComponentName+" task", "error", err)
		b.stateMu.Lock()
		b.stopMonitor = nil
		b.monitorDone = nil
		b.stateMu.Unlock()
		close(monitorDone)
		return
	}

	go b.monitorExit(ctx, exitCh, stopMonitor, monitorDone)
}

// monitorExit watches for task exits and triggers restarts.
func (b *BaseComponent) monitorExit(ctx context.Context, exitCh <-chan containerd.ExitStatus, stopMonitor <-chan struct{}, monitorDone chan struct{}) {
	defer close(monitorDone)

	for {
		select {
		case <-stopMonitor:
			b.Log.Debug(b.ComponentName + " exit monitor stopped")
			return

		case exitStatus := <-exitCh:
			b.Log.Warn(b.ComponentName+" process exited unexpectedly",
				"exit_code", exitStatus.ExitCode(),
				"exit_time", exitStatus.ExitTime(),
			)

			// Attempt restart
			if err := b.handleRestart(ctx, stopMonitor); err != nil {
				b.Log.Error("failed to restart "+b.ComponentName, "error", err)
				return
			}

			// Get new exit channel for the restarted task
			b.stateMu.Lock()
			task := b.task
			b.stateMu.Unlock()

			if task == nil {
				return
			}
			newExitCh, err := task.Wait(ctx)
			if err != nil {
				b.Log.Error("failed to get exit channel after restart", "error", err)
				return
			}
			exitCh = newExitCh
		}
	}
}

// handleRestart handles restarting the component after an unexpected exit.
func (b *BaseComponent) handleRestart(ctx context.Context, stopMonitor <-chan struct{}) error {
	b.stateMu.Lock()
	policy := b.RestartPolicy
	lastRestartAt := b.lastRestartAt
	restartCount := b.restartCount
	container := b.container
	task := b.task
	b.stateMu.Unlock()

	if container == nil {
		return fmt.Errorf("cannot restart %s: container is nil", b.ComponentName)
	}

	// Reset restart count if enough time has passed
	if time.Since(lastRestartAt) > policy.ResetWindow {
		restartCount = 0
	}

	restartCount++
	if policy.MaxRestarts > 0 && restartCount > policy.MaxRestarts {
		return fmt.Errorf("exceeded maximum restart attempts (%d)", policy.MaxRestarts)
	}

	// Calculate backoff
	backoff := policy.BackoffBase * time.Duration(1<<(restartCount-1))
	if backoff > policy.BackoffMax {
		backoff = policy.BackoffMax
	}

	b.Log.Info("restarting "+b.ComponentName+" after backoff",
		"restart_count", restartCount,
		"backoff", backoff,
	)

	select {
	case <-time.After(backoff):
	case <-stopMonitor:
		return fmt.Errorf("restart cancelled")
	case <-ctx.Done():
		return ctx.Err()
	}

	b.stateMu.Lock()
	b.restartCount = restartCount
	b.lastRestartAt = time.Now()
	b.stateMu.Unlock()

	ctx = namespaces.WithNamespace(ctx, b.Namespace)

	// Clean up old task
	if task != nil {
		deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		task.Delete(deleteCtx)
		cancel()
		b.stateMu.Lock()
		b.task = nil
		b.stateMu.Unlock()
	}

	// Create and start new task using the callback
	if b.CreateTask == nil {
		return fmt.Errorf("CreateTask callback not set")
	}

	newTask, err := b.CreateTask(ctx, container)
	if err != nil {
		return fmt.Errorf("failed to create new %s task: %w", b.ComponentName, err)
	}

	if err := newTask.Start(ctx); err != nil {
		newTask.Delete(ctx)
		return fmt.Errorf("failed to start new %s task: %w", b.ComponentName, err)
	}

	b.stateMu.Lock()
	b.task = newTask
	b.stateMu.Unlock()

	// Wait for component to be ready
	if b.GetReadyPort != nil {
		port := b.GetReadyPort()
		if err := b.WaitForReady(ctx, "localhost", port); err != nil {
			b.Log.Warn(b.ComponentName+" readiness check failed after restart", "error", err)
		}
	}

	b.Log.Info(b.ComponentName+" restarted successfully", "restart_count", restartCount)
	return nil
}
