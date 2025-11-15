package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// CleanupResult contains information about containers cleaned up during orphan removal
type CleanupResult struct {
	// DeletedContainers contains IDs of containers successfully removed
	DeletedContainers []string
	// FailedContainers contains IDs and errors for containers that failed to be removed
	FailedContainers map[string]error
}

// ContainerWatchdog periodically checks that containers in containerd match
// what is expected by sandbox entities. It removes orphaned containers that
// shouldn't exist, acting as a safety mechanism to keep the container runtime clean.
type ContainerWatchdog struct {
	Log *slog.Logger
	CC  *containerd.Client
	EAC *entityserver_v1alpha.EntityAccessClient

	Namespace string
	// CheckInterval is how often to check for orphaned containers
	CheckInterval time.Duration
	// GraceWindow is how long to wait before removing containers from non-running sandboxes
	GraceWindow time.Duration

	cancel context.CancelFunc
}

// Start begins the periodic container cleanup process
func (w *ContainerWatchdog) Start(ctx context.Context) {
	if w.CheckInterval == 0 {
		w.CheckInterval = 5 * time.Minute
	}

	w.Log.Info("starting container watchdog", "check_interval", w.CheckInterval)

	ctx, w.cancel = context.WithCancel(ctx)

	go w.monitor(ctx)
}

// Stop gracefully stops the watchdog
func (w *ContainerWatchdog) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

// monitor runs the periodic cleanup loop
func (w *ContainerWatchdog) monitor(ctx context.Context) {
	ticker := time.NewTicker(w.CheckInterval)
	defer ticker.Stop()

	// Run an initial cleanup on startup
	result, err := w.cleanupOrphanedContainers(ctx)
	if err != nil {
		w.Log.Error("initial watchdog cleanup failed", "error", err)
	} else if len(result.DeletedContainers) > 0 || len(result.FailedContainers) > 0 {
		w.Log.Info("initial watchdog cleanup complete",
			"deleted", len(result.DeletedContainers),
			"failed", len(result.FailedContainers))
	}

	for {
		select {
		case <-ticker.C:
			result, err := w.cleanupOrphanedContainers(ctx)
			if err != nil {
				w.Log.Error("watchdog cleanup failed", "error", err)
			} else if len(result.DeletedContainers) > 0 || len(result.FailedContainers) > 0 {
				w.Log.Info("watchdog cleanup complete",
					"deleted", len(result.DeletedContainers),
					"failed", len(result.FailedContainers))
			}
		case <-ctx.Done():
			w.Log.Info("container watchdog stopped")
			return
		}
	}
}

// cleanupOrphanedContainers removes containers not associated with Running sandboxes.
// Returns a CleanupResult containing lists of successfully deleted and failed containers.
func (w *ContainerWatchdog) cleanupOrphanedContainers(ctx context.Context) (*CleanupResult, error) {
	w.Log.Debug("watchdog checking for orphaned containers")

	result := &CleanupResult{
		DeletedContainers: []string{},
		FailedContainers:  make(map[string]error),
	}

	// Create a timeout for the cleanup operation
	cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cleanupCtx = namespaces.WithNamespace(cleanupCtx, w.Namespace)

	// List all containers in the namespace
	containerList, err := w.CC.Containers(cleanupCtx)
	if err != nil {
		return result, fmt.Errorf("failed to list containers: %w", err)
	}

	// Build a set of valid container IDs from Running sandboxes
	validContainers := make(map[string]bool)

	resp, err := w.EAC.List(cleanupCtx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return result, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	now := time.Now()
	graceWindow := w.GraceWindow
	if graceWindow == 0 {
		graceWindow = 3 * time.Minute
	}

	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		ent := e.Entity()

		// Consider all non DEAD sandboes as valid.
		isRunning := sb.Status != compute.DEAD

		// Also track containers for non-running sandboxes if they were updated recently
		// This gives sandboxes time to transition states without having their containers cleaned up
		isRecentlyUpdated := false
		if !isRunning {
			updatedAt := ent.GetUpdatedAt()
			isRecentlyUpdated = now.Sub(updatedAt) < graceWindow
		}

		if isRunning || isRecentlyUpdated {
			// Add pause container
			pauseID := pauseContainerId(sb.ID)
			validContainers[pauseID] = true

			// Add subcontainers
			for _, container := range sb.Spec.Container {
				containerID := fmt.Sprintf("%s-%s", containerPrefix(sb.ID), container.Name)
				validContainers[containerID] = true
			}

			if isRecentlyUpdated {
				updatedAt := ent.GetUpdatedAt()
				w.Log.Debug("granting grace period to recently updated sandbox",
					"sandbox_id", sb.ID,
					"status", sb.Status,
					"updated_at", updatedAt,
					"age", now.Sub(updatedAt))
			}
		}
	}

	// Identify orphaned containers
	var orphanedContainers []containerd.Container
	for _, container := range containerList {
		containerID := container.ID()

		// Skip if this is a valid container
		if validContainers[containerID] {
			continue
		}

		// Check labels to see if this is a sandbox container
		labels, err := container.Labels(cleanupCtx)
		if err != nil {
			w.Log.Warn("failed to get container labels, skipping", "id", containerID, "error", err)
			continue
		}

		// Skip if not a sandbox container (check for our labels)
		if _, ok := labels[sandboxEntityLabel]; !ok {
			continue
		}

		w.Log.Info("watchdog found orphaned container", "id", containerID, "labels", labels)
		orphanedContainers = append(orphanedContainers, container)
	}

	if len(orphanedContainers) == 0 {
		return result, nil
	}

	// Phase 1: Send SIGQUIT to all orphaned containers to give them a chance to shutdown gracefully
	w.Log.Info("sending SIGQUIT to orphaned containers", "count", len(orphanedContainers))
	for _, container := range orphanedContainers {
		containerID := container.ID()
		task, err := container.Task(cleanupCtx, nil)
		if err == nil && task != nil {
			if killErr := task.Kill(cleanupCtx, 3); killErr != nil { // SIGQUIT = 3
				w.Log.Debug("failed to send SIGQUIT to task", "id", containerID, "error", killErr)
			} else {
				w.Log.Debug("sent SIGQUIT to task", "id", containerID)
			}
		}
	}

	// Phase 2: Wait 5 seconds for graceful shutdown
	w.Log.Debug("waiting 5 seconds for graceful shutdown")
	time.Sleep(5 * time.Second)

	// Phase 3: Check which containers are still alive and force kill them
	for _, container := range orphanedContainers {
		containerID := container.ID()

		task, err := container.Task(cleanupCtx, nil)
		stillAlive := err == nil && task != nil

		if stillAlive {
			// Container is still alive, send SIGKILL
			w.Log.Info("container still alive after SIGQUIT, sending SIGKILL", "id", containerID)
			if killErr := task.Kill(cleanupCtx, 9); killErr != nil { // SIGKILL = 9
				w.Log.Debug("failed to send SIGKILL to task", "id", containerID, "error", killErr)
			}
		}

		// Aggressively remove the container
		if err := w.removeContainer(cleanupCtx, container); err != nil {
			w.Log.Error("watchdog failed to remove orphaned container", "id", containerID, "error", err)
			result.FailedContainers[containerID] = err
		} else {
			w.Log.Info("watchdog successfully removed orphaned container", "id", containerID)
			result.DeletedContainers = append(result.DeletedContainers, containerID)
		}
	}

	return result, nil
}

// removeContainer removes a container and its task.
// Note: The task should already have been killed before calling this function.
func (w *ContainerWatchdog) removeContainer(ctx context.Context, container containerd.Container) error {
	containerID := container.ID()

	// Try to delete any task first
	task, err := container.Task(ctx, nil)
	if err == nil && task != nil {
		// Try to delete the task (it should already be dead from SIGQUIT/SIGKILL)
		_, delErr := task.Delete(ctx, containerd.WithProcessKill)
		if delErr != nil {
			w.Log.Debug("failed to delete task during watchdog cleanup", "id", containerID, "error", delErr)
		}
	}

	// Delete the container with snapshot cleanup
	err = container.Delete(ctx, containerd.WithSnapshotCleanup)
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("failed to delete container: %w", err)
	}

	return nil
}
