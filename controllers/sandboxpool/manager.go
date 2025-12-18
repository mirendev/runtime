package sandboxpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
)

const (
	// MaxPoolSize is the maximum number of sandboxes allowed in a pool
	// This prevents runaway growth even if there are bugs in scaling logic
	MaxPoolSize = 20
)

// Manager reconciles SandboxPool entities by ensuring the actual number of
// sandbox instances matches the desired number specified in the pool.
// Implements controller.ReconcileControllerI[*compute_v1alpha.SandboxPool]
type Manager struct {
	log    *slog.Logger
	eac    *entityserver_v1alpha.EntityAccessClient
	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a new SandboxPoolManager
func NewManager(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
) *Manager {
	return &Manager{
		log: log.With("module", "sandboxpool"),
		eac: eac,
	}
}

// Init initializes the controller (required by ReconcileControllerI)
func (m *Manager) Init(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	// Start background scale-down monitor
	go m.runScaleDownMonitor(m.ctx)
	return nil
}

// Reconcile brings the actual sandbox state in line with the desired state
// specified in the pool entity.
// This method is called by the controller framework for both Add and Update events.
func (m *Manager) Reconcile(ctx context.Context, pool *compute_v1alpha.SandboxPool, meta *entity.Meta) error {
	m.log.Debug("reconciling pool",
		"pool", pool.ID,
		"service", pool.Service,
		"desired", pool.DesiredInstances)

	// Get all sandboxes for this pool
	sandboxes, err := m.listSandboxes(ctx, pool)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	// Count RUNNING and PENDING as "actual" (prevents duplicates while sandboxes boot)
	// Count only RUNNING as "ready" (sandboxes serving traffic)
	// We exclude STOPPED (being retired), DEAD (failed), and "" (uninitialized)
	// This must happen BEFORE cooldown check so current/ready counts are always up-to-date
	actual := int64(0)
	ready := int64(0)
	for _, sbm := range sandboxes {
		if sbm.sandbox.Status == compute_v1alpha.RUNNING || sbm.sandbox.Status == compute_v1alpha.PENDING {
			actual++
		}
		if sbm.sandbox.Status == compute_v1alpha.RUNNING {
			ready++
		}
	}

	// Detect recent crashes (sandboxes that died within 60s of creation)
	newCrashes := m.countQuickCrashes(sandboxes, pool.LastCrashTime)
	if newCrashes > 0 {
		pool.ConsecutiveCrashCount += int64(newCrashes)
		pool.LastCrashTime = time.Now()
		pool.CooldownUntil = m.calculateBackoff(pool.ConsecutiveCrashCount)

		m.log.Warn("crash detected, entering cooldown",
			"pool", pool.ID,
			"new_crashes", newCrashes,
			"consecutive_crashes", pool.ConsecutiveCrashCount,
			"cooldown_until", pool.CooldownUntil)

		// Pool crash state fields are already updated, framework will apply via entity.Diff
	}

	// Check if pool is in cooldown
	if !pool.CooldownUntil.IsZero() && time.Now().Before(pool.CooldownUntil) {
		// Check if pool is unreferenced (no versions reference it)
		// Unreferenced pools should be allowed to scale to 0 even during cooldown
		isUnreferenced := len(pool.ReferencedByVersions) == 0

		// Reset DesiredInstances to prevent activator-driven accumulation
		// Allow desired: 0 for unreferenced pools (deployment cleanup)
		targetDesired := int64(1)
		if isUnreferenced {
			targetDesired = 0
		}

		if pool.DesiredInstances != targetDesired {
			m.log.Info("resetting DesiredInstances during cooldown",
				"pool", pool.ID,
				"old", pool.DesiredInstances,
				"new", targetDesired,
				"unreferenced", isUnreferenced,
				"cooldown_until", pool.CooldownUntil)
			pool.DesiredInstances = targetDesired
			// DesiredInstances updated, framework will apply via entity.Diff
		}

		// Update current/ready counts even during cooldown, then skip scaling logic
		return m.updatePoolStatus(ctx, pool, actual, ready, meta)
	}

	// Check for healthy sandbox to reset crash counter
	if pool.ConsecutiveCrashCount > 0 && m.hasHealthySandbox(sandboxes, pool.ConsecutiveCrashCount) {
		m.log.Info("healthy sandbox detected, resetting crash counter",
			"pool", pool.ID,
			"previous_crash_count", pool.ConsecutiveCrashCount)
		pool.ConsecutiveCrashCount = 0
		pool.LastCrashTime = time.Time{}
		pool.CooldownUntil = time.Time{}
		// Pool crash state fields reset, framework will apply via entity.Diff
	}

	// actual and ready were already counted at the top of Reconcile
	desired := pool.DesiredInstances

	// Cap desired at MaxPoolSize as a defensive measure
	if desired > MaxPoolSize {
		m.log.Warn("pool desired instances exceeds maximum, capping",
			"pool", pool.ID,
			"desired", desired,
			"max_size", MaxPoolSize)
		desired = MaxPoolSize
	}

	m.log.Debug("sandbox counts",
		"pool", pool.ID,
		"actual", actual,
		"ready", ready,
		"desired", desired)

	// Scale up if needed
	if actual < desired {
		toCreate := desired - actual
		m.log.Info("scaling up pool",
			"pool", pool.ID,
			"service", pool.Service,
			"current", actual,
			"desired", desired,
			"creating", toCreate)

		// Determine which instance numbers to create
		// Collect existing instance numbers
		existingInstances := make(map[int]bool)
		for _, sbm := range sandboxes {
			// Get instance number from sandbox metadata
			resp, err := m.eac.Get(ctx, sbm.sandbox.ID.String())
			if err != nil {
				m.log.Warn("failed to get sandbox metadata", "sandbox", sbm.sandbox.ID, "error", err)
				continue
			}

			var md core_v1alpha.Metadata
			md.Decode(resp.Entity().Entity())

			if instanceStr, ok := md.Labels.Get("instance"); ok {
				var instanceNum int
				fmt.Sscanf(instanceStr, "%d", &instanceNum)
				existingInstances[instanceNum] = true
			}
		}

		// Find next available instance numbers
		instancesNeeded := make([]int, 0, toCreate)
		for instanceNum := 0; len(instancesNeeded) < int(toCreate); instanceNum++ {
			if !existingInstances[instanceNum] {
				instancesNeeded = append(instancesNeeded, instanceNum)
			}
		}

		// Create sandboxes with assigned instance numbers
		for _, instanceNum := range instancesNeeded {
			if err := m.createSandbox(ctx, pool, instanceNum); err != nil {
				m.log.Error("failed to create sandbox",
					"pool", pool.ID,
					"instance", instanceNum,
					"error", err)
				// Continue - partial scaling is acceptable
			}
		}

		// Recount after creating sandboxes
		sandboxes, err = m.listSandboxes(ctx, pool)
		if err != nil {
			return fmt.Errorf("failed to list sandboxes after scale up: %w", err)
		}
		actual = 0
		ready = 0
		for _, sbm := range sandboxes {
			if sbm.sandbox.Status == compute_v1alpha.RUNNING || sbm.sandbox.Status == compute_v1alpha.PENDING {
				actual++
			}
			if sbm.sandbox.Status == compute_v1alpha.RUNNING {
				ready++
			}
		}
	}

	// Scale down if needed
	if actual > desired {
		toStop := actual - desired
		m.log.Info("scaling down pool",
			"pool", pool.ID,
			"service", pool.Service,
			"current", actual,
			"desired", desired,
			"stopping", toStop)

		if err := m.scaleDown(ctx, pool, sandboxes, toStop); err != nil {
			m.log.Error("failed to scale down",
				"pool", pool.ID,
				"error", err)
			// Continue - update status with current state
		}

		// Recount after stopping sandboxes
		sandboxes, err = m.listSandboxes(ctx, pool)
		if err != nil {
			return fmt.Errorf("failed to list sandboxes after scale down: %w", err)
		}
		actual = 0
		ready = 0
		for _, sbm := range sandboxes {
			if sbm.sandbox.Status == compute_v1alpha.RUNNING || sbm.sandbox.Status == compute_v1alpha.PENDING {
				actual++
			}
			if sbm.sandbox.Status == compute_v1alpha.RUNNING {
				ready++
			}
		}
	}

	// Update pool status and propagate changes to entity for framework to persist
	return m.updatePoolStatus(ctx, pool, actual, ready, meta)
}

// Delete handles pool deletion by stopping all sandboxes in the pool.
// This prevents orphaned sandboxes when a pool is deleted.
// Implements controller.DeletingReconcileController
func (m *Manager) Delete(ctx context.Context, poolID entity.Id) error {
	m.log.Info("deleting pool, stopping all sandboxes", "pool", poolID)

	// We need to query all sandboxes with this pool label and stop them
	// Query all sandboxes (we can't filter by pool label in the query, so we filter in memory)
	resp, err := m.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	stoppedCount := 0
	deletedCount := 0
	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Check if this sandbox belongs to the deleted pool
		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())
		poolLabel, _ := md.Labels.Get("pool")
		if poolLabel != poolID.String() {
			continue // Not our pool's sandbox
		}

		// Delete the sandbox entity
		m.log.Info("deleting sandbox from deleted pool", "sandbox", sb.ID, "pool", poolID)
		if _, err := m.eac.Delete(ctx, sb.ID.String()); err != nil {
			m.log.Error("failed to delete sandbox from deleted pool",
				"sandbox", sb.ID,
				"pool", poolID,
				"error", err)
			continue
		}

		deletedCount++
	}

	m.log.Info("cleaned up sandboxes from deleted pool", "pool", poolID, "stopped", stoppedCount, "deleted", deletedCount)
	return nil
}

type sandboxWithMeta struct {
	sandbox   *compute_v1alpha.Sandbox
	createdAt time.Time
	updatedAt time.Time
}

// listSandboxes returns all sandboxes for a pool with their metadata
func (m *Manager) listSandboxes(ctx context.Context, pool *compute_v1alpha.SandboxPool) ([]*sandboxWithMeta, error) {
	// Query sandboxes by version index (reduces O(N) to O(N_version))
	// We can now query by the nested sandbox.spec.version field directly!
	resp, err := m.eac.List(ctx, entity.Ref(compute_v1alpha.SandboxSpecVersionId, pool.SandboxSpec.Version))
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	var sandboxes []*sandboxWithMeta

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Filter by service and pool labels (labels not indexed, must filter in-memory)
		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		serviceLabel, _ := md.Labels.Get("service")
		if serviceLabel != pool.Service {
			continue
		}

		poolLabel, _ := md.Labels.Get("pool")
		if poolLabel != pool.ID.String() {
			continue
		}

		// Copy into a new variable so each entry gets a distinct pointer
		sbCopy := sb
		sandboxes = append(sandboxes, &sandboxWithMeta{
			sandbox:   &sbCopy,
			createdAt: time.UnixMilli(ent.CreatedAt()),
			updatedAt: time.UnixMilli(ent.UpdatedAt()),
		})
	}

	return sandboxes, nil
}

// createSandbox creates a new sandbox from the pool's SandboxSpec template
func (m *Manager) createSandbox(ctx context.Context, pool *compute_v1alpha.SandboxPool, instanceNum int) error {
	// Generate sandbox name using pool's prefix, fallback to "sb" if not set
	prefix := pool.SandboxPrefix
	if prefix == "" {
		prefix = "sb"
	}
	sbName := idgen.GenNS(prefix)

	// Clone the SandboxSpec into a Sandbox entity
	sb := compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec:   pool.SandboxSpec,
	}

	// Build sandbox labels: merge pool.SandboxLabels with required labels
	sandboxLabels := types.LabelSet(
		"service", pool.Service,
		"pool", pool.ID.String(),
		"instance", fmt.Sprintf("%d", instanceNum),
	)

	// Merge in labels from pool.SandboxLabels
	sandboxLabels = append(sandboxLabels, pool.SandboxLabels...)

	// Create entity with metadata
	resp, err := m.eac.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name:   sbName,
			Labels: sandboxLabels,
		}).Encode,
		entity.DBId, entity.Id("sandbox/"+sbName),
		sb.Encode,
	).Attrs())
	if err != nil {
		return fmt.Errorf("failed to create sandbox entity: %w", err)
	}

	m.log.Info("created sandbox",
		"sandbox", resp.Id(),
		"pool", pool.ID,
		"service", pool.Service,
		"instance", instanceNum)

	return nil
}

// scaleDown stops excess sandboxes to bring actual count down to desired count.
// This is called when actual > desired (proactive scale-down).
// Sandboxes are retired in priority order: oldest LastActivity first.
func (m *Manager) scaleDown(ctx context.Context, pool *compute_v1alpha.SandboxPool, sandboxes []*sandboxWithMeta, count int64) error {
	now := time.Now()

	// Collect all RUNNING sandboxes as candidates
	type candidate struct {
		sb           *compute_v1alpha.Sandbox
		lastActivity time.Time
	}

	var candidates []candidate
	for _, sbm := range sandboxes {
		// Only consider RUNNING sandboxes
		if sbm.sandbox.Status != compute_v1alpha.RUNNING {
			continue
		}

		lastActivity := sbm.sandbox.LastActivity
		// If LastActivity is not set, treat as current time (most recently active)
		if lastActivity.IsZero() {
			lastActivity = now
		}

		candidates = append(candidates, candidate{
			sb:           sbm.sandbox,
			lastActivity: lastActivity,
		})
	}

	if len(candidates) == 0 {
		m.log.Debug("no RUNNING sandboxes available to retire", "pool", pool.ID)
		return nil
	}

	// Ensure we don't try to stop more than available
	if int64(len(candidates)) < count {
		m.log.Warn("not enough sandboxes to scale down",
			"pool", pool.ID,
			"requested", count,
			"available", len(candidates))
		count = int64(len(candidates))
	}

	// Sort by LastActivity (oldest first) - retire least recently active sandboxes
	// Use a simple bubble sort since count is typically small
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].lastActivity.After(candidates[j].lastActivity) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Stop the first 'count' sandboxes (those with oldest LastActivity)
	stopped := int64(0)
	for i := int64(0); i < count && int(i) < len(candidates); i++ {
		sb := candidates[i].sb

		m.log.Info("retiring sandbox",
			"pool", pool.ID,
			"sandbox", sb.ID,
			"last_activity", candidates[i].lastActivity)

		// Mark sandbox as STOPPED
		if _, err := m.eac.Patch(ctx, entity.New(
			entity.DBId, sb.ID,
			(&compute_v1alpha.Sandbox{
				Status: compute_v1alpha.STOPPED,
			}).Encode,
		).Attrs(), 0); err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				m.log.Warn("sandbox already deleted during scale-down",
					"pool", pool.ID,
					"sandbox", sb.ID)
			} else {
				m.log.Error("failed to stop sandbox",
					"pool", pool.ID,
					"sandbox", sb.ID,
					"error", err)
			}
			continue
		}

		stopped++
	}

	m.log.Info("scale-down complete",
		"pool", pool.ID,
		"requested", count,
		"stopped", stopped)

	return nil
}

// updatePoolStatus updates the pool's CurrentInstances and ReadyInstances fields,
// then propagates all pool changes back to meta.Entity for the framework to diff and persist.
// When called with a nil meta (e.g., during testing), it falls back to direct persistence.
func (m *Manager) updatePoolStatus(ctx context.Context, pool *compute_v1alpha.SandboxPool, current, ready int64, meta *entity.Meta) error {
	// Update status counters
	if pool.CurrentInstances != current || pool.ReadyInstances != ready {
		pool.CurrentInstances = current
		pool.ReadyInstances = ready

		m.log.Debug("updated pool status",
			"pool", pool.ID,
			"current", current,
			"ready", ready)
	}

	// Propagate all pool changes back to the entity for the framework to diff and persist.
	// We must explicitly set CurrentInstances and ReadyInstances even when they are 0,
	// since Encode() skips "empty" values which includes 0.
	if meta == nil {
		return nil
	}

	if meta.Entity == nil {
		meta.Entity = entity.New(pool.Encode())
	} else {
		meta.Update(pool.Encode())
	}
	// Explicitly set status fields to ensure 0 values are persisted
	meta.Update([]entity.Attr{
		entity.Int64(compute_v1alpha.SandboxPoolCurrentInstancesId, current),
		entity.Int64(compute_v1alpha.SandboxPoolReadyInstancesId, ready),
	})

	return nil
}

// runScaleDownMonitor periodically checks all pools for idle sandboxes that exceed
// their ScaleDownDelay, and proactively decrements DesiredInstances to trigger scale-down.
// Also cleans up empty pools that have been idle for over an hour.
func (m *Manager) runScaleDownMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	m.log.Info("scale-down monitor started")

	for {
		select {
		case <-ctx.Done():
			m.log.Info("scale-down monitor stopped")
			return
		case <-ticker.C:
			if err := m.checkAllPoolsForScaleDown(ctx); err != nil {
				m.log.Error("scale-down check failed", "error", err)
			}
			if err := m.cleanupEmptyPools(ctx); err != nil {
				m.log.Error("pool cleanup failed", "error", err)
			}
		}
	}
}

// checkAllPoolsForScaleDown examines all pools and decrements DesiredInstances
// if idle sandboxes exceed their ScaleDownDelay threshold.
func (m *Manager) checkAllPoolsForScaleDown(ctx context.Context) error {
	// List all sandbox pools
	resp, err := m.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return fmt.Errorf("failed to list pools: %w", err)
	}

	for _, ent := range resp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		// Get AppVersion to derive concurrency config
		verResp, err := m.eac.Get(ctx, pool.SandboxSpec.Version.String())
		if err != nil {
			m.log.Error("failed to get version for pool", "pool", pool.ID, "error", err)
			continue
		}
		var ver core_v1alpha.AppVersion
		ver.Decode(verResp.Entity().Entity())

		// Get service concurrency config
		svcConcurrency, err := core_v1alpha.GetServiceConcurrency(&ver, pool.Service)
		if err != nil {
			m.log.Error("failed to get service concurrency", "pool", pool.ID, "error", err)
			continue
		}

		// Use concurrency strategy to determine scale-down delay
		strategy := concurrency.NewStrategy(&svcConcurrency)
		scaleDownDelay := strategy.ScaleDownDelay()

		// Skip pools that never scale down (fixed mode)
		if scaleDownDelay == 0 {
			continue
		}

		// Get desired instances from strategy (for fixed mode minimum)
		minInstances := int64(strategy.DesiredInstances())

		// Check if this pool has idle sandboxes that should be retired
		if err := m.checkPoolForScaleDown(ctx, &pool, scaleDownDelay, minInstances); err != nil {
			m.log.Error("failed to check pool for scale-down",
				"pool", pool.ID,
				"error", err)
		}
	}

	return nil
}

// checkPoolForScaleDown examines a single pool and decrements DesiredInstances
// if there are idle sandboxes that exceed the ScaleDownDelay threshold.
func (m *Manager) checkPoolForScaleDown(ctx context.Context, pool *compute_v1alpha.SandboxPool, scaleDownDelay time.Duration, minInstances int64) error {
	// Get all sandboxes for this pool
	sandboxes, err := m.listSandboxes(ctx, pool)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	now := time.Now()
	idleCount := int64(0)

	// Count RUNNING sandboxes that are idle beyond ScaleDownDelay
	for _, sbm := range sandboxes {
		if sbm.sandbox.Status != compute_v1alpha.RUNNING {
			continue
		}

		lastActivity := sbm.sandbox.LastActivity
		if lastActivity.IsZero() {
			// No activity recorded yet - treat as recently active
			continue
		}

		idleTime := now.Sub(lastActivity)
		if idleTime > scaleDownDelay {
			idleCount++
		}
	}

	// If we have idle sandboxes and we're above minimum instances, decrement desired
	if idleCount > 0 && pool.DesiredInstances > minInstances {
		// Only decrement by the number of idle sandboxes, but respect minimum
		newDesired := pool.DesiredInstances - idleCount
		if newDesired < minInstances {
			newDesired = minInstances
		}

		if newDesired < pool.DesiredInstances {
			m.log.Info("proactively scaling down pool",
				"pool", pool.ID,
				"service", pool.Service,
				"current_desired", pool.DesiredInstances,
				"new_desired", newDesired,
				"idle_sandboxes", idleCount)

			// Use Patch to update just the DesiredInstances field
			// This explicitly sets the value, even when newDesired is 0 (scale-to-zero)
			attrs := []entity.Attr{
				entity.Ref(entity.DBId, pool.ID),
				entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, newDesired),
			}

			if _, err := m.eac.Patch(ctx, attrs, 0); err != nil {
				return fmt.Errorf("failed to update pool desired instances: %w", err)
			}
		}
	}

	return nil
}

// cleanupEmptyPools deletes pools that have desired_instances=0, no associated sandboxes,
// and haven't been updated in over an hour. This matches the sandbox controller's behavior
// of deleting DEAD sandboxes after an hour.
func (m *Manager) cleanupEmptyPools(ctx context.Context) error {
	resp, err := m.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return fmt.Errorf("failed to list pools: %w", err)
	}

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	for _, ent := range resp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		// Only consider pools with desired_instances=0
		if pool.DesiredInstances != 0 {
			continue
		}

		// Check if pool has been idle for over an hour
		updatedAt := time.UnixMilli(ent.UpdatedAt())
		if updatedAt.After(oneHourAgo) {
			continue
		}

		// Check if pool has any associated sandboxes
		sandboxes, err := m.listSandboxes(ctx, &pool)
		if err != nil {
			m.log.Error("failed to list sandboxes for pool cleanup", "pool", pool.ID, "error", err)
			continue
		}

		if len(sandboxes) > 0 {
			continue
		}

		// Check if the pool is still referenced by a current app version.
		// Even if the pool is empty and scaled to zero, we should keep it
		// if it's the active pool for an app (waiting for the next request).
		if m.isPoolReferencedByCurrentVersion(ctx, &pool) {
			m.log.Debug("skipping cleanup of empty pool still referenced by current version",
				"pool", pool.ID,
				"service", pool.Service,
				"referenced_versions", pool.ReferencedByVersions)
			continue
		}

		// Pool is empty, scaled to zero, idle for over an hour, and not referenced
		// by any current app version - safe to delete
		m.log.Info("cleaning up empty pool", "pool", pool.ID, "service", pool.Service, "age", now.Sub(updatedAt))

		if _, err := m.eac.Delete(ctx, pool.ID.String()); err != nil {
			m.log.Error("failed to delete empty pool", "pool", pool.ID, "error", err)
			continue
		}

		m.log.Info("deleted empty pool", "pool", pool.ID)
	}

	return nil
}

// countQuickCrashes counts sandboxes that died within 60 seconds of creation
// and occurred after lastCrashTime
func (m *Manager) countQuickCrashes(sandboxes []*sandboxWithMeta, lastCrashTime time.Time) int64 {
	count := int64(0)
	crashThreshold := 60 * time.Second

	for _, sbm := range sandboxes {
		if sbm.sandbox.Status != compute_v1alpha.DEAD {
			continue
		}

		// Check if this is a quick crash (died within 60s of creation)
		lifetime := sbm.updatedAt.Sub(sbm.createdAt)

		if lifetime >= crashThreshold {
			continue // Lived long enough, not a quick crash
		}

		// Check if this crash is new (after lastCrashTime)
		if !lastCrashTime.IsZero() && !sbm.updatedAt.After(lastCrashTime) {
			continue // Already counted this crash
		}

		count++
	}

	return count
}

// backoffDuration calculates the exponential backoff duration based on consecutive crash count
// Uses exponential backoff: 10s, 20s, 40s, 80s, 160s, 320s, 640s, ...
// Capped at 15 minutes
func backoffDuration(crashCount int64) time.Duration {
	if crashCount <= 0 {
		return 0
	}

	// Cap crash count to prevent bit-shift overflow (2^63 would overflow)
	// At crashCount=10: 2^9 * 10s = 5120s, already well over 15min cap
	if crashCount > 20 {
		crashCount = 20
	}

	// 2^(n-1) * 10 seconds
	backoffSeconds := (1 << uint(crashCount-1)) * 10
	backoff := time.Duration(backoffSeconds) * time.Second

	maxBackoff := 15 * time.Minute
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}

// calculateBackoff returns the absolute time when cooldown ends based on consecutive crash count
func (m *Manager) calculateBackoff(crashCount int64) time.Time {
	d := backoffDuration(crashCount)
	if d == 0 {
		return time.Time{}
	}
	return time.Now().Add(d)
}

// hasHealthySandbox checks if there's a sandbox that has been running long enough
// to prove stability. The required uptime scales with the crash count.
func (m *Manager) hasHealthySandbox(sandboxes []*sandboxWithMeta, crashCount int64) bool {
	// Calculate required uptime based on current backoff
	requiredUptime := backoffDuration(crashCount)

	// Minimum 2 minutes required uptime
	if requiredUptime < 2*time.Minute {
		requiredUptime = 2 * time.Minute
	}

	now := time.Now()
	for _, sbm := range sandboxes {
		if sbm.sandbox.Status == compute_v1alpha.RUNNING {
			uptime := now.Sub(sbm.createdAt)
			if uptime >= requiredUptime {
				return true
			}
		}
	}

	return false
}

// isPoolReferencedByCurrentVersion checks if any of the pool's referenced versions
// is still the current version for its app. This prevents deleting pools that are
// legitimately at scale-to-zero but should spin up on the next request.
func (m *Manager) isPoolReferencedByCurrentVersion(ctx context.Context, pool *compute_v1alpha.SandboxPool) bool {
	for _, versionID := range pool.ReferencedByVersions {
		// Fetch the app version to get its app reference
		versionResp, err := m.eac.Get(ctx, versionID.String())
		if err != nil {
			// Version might have been deleted - that's fine, it's not current
			if errors.Is(err, cond.ErrNotFound{}) {
				continue
			}
			m.log.Warn("failed to get version when checking pool references",
				"version", versionID,
				"pool", pool.ID,
				"error", err)
			continue
		}

		var version core_v1alpha.AppVersion
		version.Decode(versionResp.Entity().Entity())

		// Fetch the app to check its current version
		appResp, err := m.eac.Get(ctx, version.App.String())
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				continue
			}
			m.log.Warn("failed to get app when checking pool references",
				"app", version.App,
				"pool", pool.ID,
				"error", err)
			continue
		}

		var app core_v1alpha.App
		app.Decode(appResp.Entity().Entity())

		// Check if this version is the app's active version
		if app.ActiveVersion == versionID {
			return true
		}
	}

	return false
}
