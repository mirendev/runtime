package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/netdb"
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
	// NodeId scopes sandbox lookups to this node so we only consider
	// sandboxes that are scheduled here when building the valid set.
	NodeId compute.NodeId
	// CheckInterval is how often to check for orphaned containers
	CheckInterval time.Duration
	// GraceWindow is how long to wait before removing containers from non-running sandboxes
	GraceWindow time.Duration
	// Subnet is used to release IP addresses when removing orphaned containers
	Subnet *netdb.Subnet

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
	result, err := w.CleanupOrphanedContainers(ctx)
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
			result, err := w.CleanupOrphanedContainers(ctx)
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

// CleanupOrphanedContainers removes containers not associated with Running sandboxes.
// Returns a CleanupResult containing lists of successfully deleted and failed containers.
func (w *ContainerWatchdog) CleanupOrphanedContainers(ctx context.Context) (*CleanupResult, error) {
	w.Log.Debug("watchdog checking for orphaned containers")

	result := &CleanupResult{
		DeletedContainers: []string{},
		FailedContainers:  make(map[string]error),
	}

	if w.NodeId == "" {
		return result, fmt.Errorf("watchdog: NodeId is required for cleanup")
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

	// Build a set of valid container IDs from sandboxes scheduled to this node,
	// plus the set of sandbox IDs the index returned at all (valid or expired) so
	// we can tell "the index has no record of this sandbox" (which warrants a
	// direct re-fetch) apart from "the index returned it but it is past grace"
	// (already authoritative — no re-fetch needed).
	validContainers := make(map[string]bool)
	indexedSandboxes := make(map[string]bool)

	resp, err := w.EAC.List(cleanupCtx, compute.Index(compute.KindSandbox, w.NodeId.Id()))
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

		indexedSandboxes[sb.ID.String()] = true

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

	// Collect the sandbox containers (those carrying our entity label) up front so
	// we can reason about the overall picture before killing or releasing anything.
	type orphanedContainer struct {
		container containerd.Container
		labels    map[string]string
	}
	type sandboxContainer struct {
		container containerd.Container
		labels    map[string]string
		sandboxID string
	}
	var sandboxContainers []sandboxContainer
	for _, container := range containerList {
		containerID := container.ID()

		// Check labels to see if this is a sandbox container
		labels, err := container.Labels(cleanupCtx)
		if err != nil {
			w.Log.Warn("failed to get container labels, skipping", "id", containerID, "error", err)
			continue
		}

		// Skip if not a sandbox container (check for our labels)
		sandboxID, ok := labels[sandboxEntityLabel]
		if !ok {
			continue
		}

		sandboxContainers = append(sandboxContainers, sandboxContainer{
			container: container,
			labels:    labels,
			sandboxID: sandboxID,
		})
	}

	// An empty node-scoped sandbox list while sandbox containers are clearly
	// running is a strong signal that the node index is lagging. We do not act on
	// the empty list directly — the per-container re-fetch below is authoritative
	// and both protects index-missed live sandboxes and still reclaims genuinely
	// drained ones — but surface it so the staleness is visible (MIR-1238).
	if len(resp.Values()) == 0 && len(sandboxContainers) > 0 {
		w.Log.Warn("watchdog: node sandbox list empty but sandbox containers exist; relying on per-container entity re-fetch",
			"sandbox_containers", len(sandboxContainers))
	}

	// Identify orphaned containers and store their labels for IP cleanup.
	var orphanedContainers []orphanedContainer
	for _, sc := range sandboxContainers {
		containerID := sc.container.ID()

		// Skip if this is a valid container
		if validContainers[containerID] {
			continue
		}

		// If the index returned this sandbox at all, it was already evaluated
		// above and found not valid (DEAD past the grace window) — that verdict
		// is authoritative, so reclaim it without a redundant re-fetch.
		//
		// If the index has no record of it, the node index may simply be lagging.
		// Re-fetch the sandbox entity directly by ID — a linearizable key lookup,
		// not an index read — before treating the container as orphaned. If the
		// sandbox still exists and is not DEAD past the grace window, the index
		// merely missed it: never kill it or release its IP (MIR-1238).
		if !indexedSandboxes[sc.sandboxID] && w.sandboxStillValid(cleanupCtx, sc.sandboxID, now, graceWindow) {
			w.Log.Warn("watchdog: container missing from node index but sandbox entity is still valid; skipping (stale index)",
				"id", containerID, "sandbox_id", sc.sandboxID)
			continue
		}

		w.Log.Info("watchdog found orphaned container", "id", containerID, "labels", sc.labels)
		orphanedContainers = append(orphanedContainers, orphanedContainer{container: sc.container, labels: sc.labels})
	}

	if len(orphanedContainers) == 0 {
		return result, nil
	}

	// Phase 1: Send SIGQUIT to all orphaned containers to give them a chance to shutdown gracefully
	w.Log.Info("sending SIGQUIT to orphaned containers", "count", len(orphanedContainers))
	for _, oc := range orphanedContainers {
		containerID := oc.container.ID()
		task, err := oc.container.Task(cleanupCtx, nil)
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
	for _, oc := range orphanedContainers {
		containerID := oc.container.ID()

		task, err := oc.container.Task(cleanupCtx, nil)
		stillAlive := err == nil && task != nil

		if stillAlive {
			// Container is still alive, send SIGKILL
			w.Log.Info("container still alive after SIGQUIT, sending SIGKILL", "id", containerID)
			if killErr := task.Kill(cleanupCtx, 9); killErr != nil { // SIGKILL = 9
				w.Log.Debug("failed to send SIGKILL to task", "id", containerID, "error", killErr)
			}
		}

		// Aggressively remove the container. Only once removal succeeds — and the
		// container's network namespace and veth are actually torn down — do we
		// return its IP to the pool. Releasing before confirmed removal can hand
		// a still-live sandbox's IP to a new sandbox, producing a duplicate
		// assignment on the bridge (MIR-1238).
		if err := w.removeContainer(cleanupCtx, oc.container); err != nil {
			w.Log.Error("watchdog failed to remove orphaned container; leaving its IP reserved", "id", containerID, "error", err)
			result.FailedContainers[containerID] = err
			continue
		}

		w.Log.Info("watchdog successfully removed orphaned container", "id", containerID)
		result.DeletedContainers = append(result.DeletedContainers, containerID)

		w.releaseContainerIPs(containerID, oc.labels)
	}

	return result, nil
}

// releaseContainerIPs returns the IP addresses recorded in a container's labels
// to the subnet pool. It must only be called after the container has been
// confirmed removed, so the addresses are no longer live on the bridge.
func (w *ContainerWatchdog) releaseContainerIPs(containerID string, labels map[string]string) {
	if w.Subnet == nil {
		return
	}
	for label, value := range labels {
		if !strings.HasPrefix(label, "runtime.computer/ip") {
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			w.Log.Warn("watchdog failed to parse IP from label", "id", containerID, "label", label, "value", value, "error", err)
			continue
		}
		if err := w.Subnet.ReleaseAddr(addr); err != nil {
			w.Log.Error("watchdog failed to release IP", "id", containerID, "addr", addr, "error", err)
		} else {
			w.Log.Debug("watchdog released IP", "id", containerID, "addr", addr)
		}
	}
}

// sandboxStillValid reports whether the sandbox with the given ID should still be
// treated as live. It re-fetches the entity directly by ID — a linearizable key
// lookup that, unlike the node-scoped index used to build the valid set, does not
// lag — so it reliably distinguishes a sandbox the index transiently omitted from
// one that is genuinely gone. A sandbox is valid if it exists and is not DEAD, or
// is DEAD but was updated within the grace window. On any non not-found error it
// returns true (fail-safe: keep the sandbox rather than risk reclaiming a live
// one).
func (w *ContainerWatchdog) sandboxStillValid(ctx context.Context, sandboxID string, now time.Time, graceWindow time.Duration) bool {
	resp, err := w.EAC.Get(ctx, sandboxID)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return false
		}
		w.Log.Warn("watchdog: failed to re-fetch sandbox entity; treating as valid (fail-safe)",
			"sandbox_id", sandboxID, "error", err)
		return true
	}
	if !resp.HasEntity() {
		return false
	}

	ent := resp.Entity().Entity()

	var sb compute.Sandbox
	sb.Decode(ent)

	if sb.Status != compute.DEAD {
		return true
	}
	return now.Sub(ent.GetUpdatedAt()) < graceWindow
}

// removeContainer removes a container and its task.
// Note: The task should already have been killed before calling this function.
func (w *ContainerWatchdog) removeContainer(ctx context.Context, container containerd.Container) error {
	containerID := container.ID()

	// Try to delete any task first
	task, err := container.Task(ctx, cleanupAttach())
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
