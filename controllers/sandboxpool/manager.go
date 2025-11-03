package sandboxpool

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
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
		log: log.With("component", "sandboxpool-manager"),
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
	// We exclude STOPPED (being retired) and DEAD (failed)
	actual := int64(0)
	ready := int64(0)
	for _, sb := range sandboxes {
		if sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.CREATED || sb.Status == compute_v1alpha.PENDING {
			actual++
		}
		if sb.Status == compute_v1alpha.RUNNING {
			ready++
		}
	}

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

		for i := int64(0); i < toCreate; i++ {
			if err := m.createSandbox(ctx, pool); err != nil {
				m.log.Error("failed to create sandbox",
					"pool", pool.ID,
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
		for _, sb := range sandboxes {
			if sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.CREATED || sb.Status == compute_v1alpha.PENDING {
				actual++
			}
			if sb.Status == compute_v1alpha.RUNNING {
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
		for _, sb := range sandboxes {
			if sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.CREATED || sb.Status == compute_v1alpha.PENDING {
				actual++
			}
			if sb.Status == compute_v1alpha.RUNNING {
				ready++
			}
		}
	}

	// Update pool status
	return m.updatePoolStatus(ctx, pool, actual, ready)
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

		// Mark sandbox as STOPPED
		m.log.Info("stopping sandbox from deleted pool", "sandbox", sb.ID, "pool", poolID)
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sb.ID.String())
		rpcE.SetAttrs(entity.New(
			(&compute_v1alpha.Sandbox{
				Status: compute_v1alpha.STOPPED,
			}).Encode,
		).Attrs())

		if _, err := m.eac.Put(ctx, &rpcE); err != nil {
			m.log.Error("failed to stop sandbox from deleted pool",
				"sandbox", sb.ID,
				"pool", poolID,
				"error", err)
			continue
		}

		stoppedCount++
	}

	m.log.Info("stopped all sandboxes from deleted pool", "pool", poolID, "stopped", stoppedCount)
	return nil
}

// listSandboxes returns all sandboxes for a pool
func (m *Manager) listSandboxes(ctx context.Context, pool *compute_v1alpha.SandboxPool) ([]*compute_v1alpha.Sandbox, error) {
	// Query sandboxes by version index (reduces O(N) to O(N_version))
	// We can now query by the nested sandbox.spec.version field directly!
	resp, err := m.eac.List(ctx, entity.Ref(compute_v1alpha.SandboxSpecVersionId, pool.SandboxSpec.Version))
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	var sandboxes []*compute_v1alpha.Sandbox

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

		sandboxes = append(sandboxes, &sb)
	}

	return sandboxes, nil
}

// createSandbox creates a new sandbox from the pool's SandboxSpec template
func (m *Manager) createSandbox(ctx context.Context, pool *compute_v1alpha.SandboxPool) error {
	// Generate sandbox name
	sbName := idgen.GenNS("sb")

	// Clone the SandboxSpec into a Sandbox entity
	sb := compute_v1alpha.Sandbox{
		Status: compute_v1alpha.CREATED,
		Spec:   pool.SandboxSpec,
	}

	// Create entity with metadata (Put without ID creates new entity)
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name: sbName,
			Labels: types.LabelSet(
				"service", pool.Service,
				"pool", pool.ID.String(),
			),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/"+sbName),
		sb.Encode,
	).Attrs())

	resp, err := m.eac.Put(ctx, &rpcE)
	if err != nil {
		return fmt.Errorf("failed to create sandbox entity: %w", err)
	}

	m.log.Info("created sandbox",
		"sandbox", resp.Id(),
		"pool", pool.ID,
		"service", pool.Service)

	return nil
}

// scaleDown stops excess sandboxes based on LastActivity and ScaleDownDelay
func (m *Manager) scaleDown(ctx context.Context, pool *compute_v1alpha.SandboxPool, sandboxes []*compute_v1alpha.Sandbox, count int64) error {
	// Get AppVersion to derive concurrency config
	verResp, err := m.eac.Get(ctx, pool.SandboxSpec.Version.String())
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}
	var ver core_v1alpha.AppVersion
	ver.Decode(verResp.Entity().Entity())

	// Get service concurrency config to determine scale-down behavior
	svcConcurrency, err := core_v1alpha.GetServiceConcurrency(&ver, pool.Service)
	if err != nil {
		return fmt.Errorf("failed to get service concurrency: %w", err)
	}

	// Use concurrency strategy to determine scale-down delay
	strategy := concurrency.NewStrategy(&svcConcurrency)
	scaleDownDelay := strategy.ScaleDownDelay()

	// Skip scale-down if ScaleDownDelay is 0 (fixed mode - never retire)
	if scaleDownDelay == 0 {
		m.log.Debug("skipping scale-down for fixed mode pool", "pool", pool.ID)
		return nil
	}

	now := time.Now()

	// Filter for candidates: RUNNING sandboxes that are idle (LastActivity + ScaleDownDelay < now)
	type candidate struct {
		sb           *compute_v1alpha.Sandbox
		idleTime     time.Duration
		lastActivity time.Time
	}

	var candidates []candidate

	for _, sb := range sandboxes {
		// Only consider RUNNING sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		// Check if idle based on LastActivity
		lastActivity := sb.LastActivity
		if lastActivity.IsZero() {
			// No activity recorded yet - treat as recently active (don't retire)
			continue
		}

		idleTime := now.Sub(lastActivity)
		if idleTime > scaleDownDelay {
			candidates = append(candidates, candidate{
				sb:           sb,
				idleTime:     idleTime,
				lastActivity: lastActivity,
			})
		}
	}

	// If we don't have enough idle sandboxes, we can't scale down the requested amount
	if int64(len(candidates)) < count {
		m.log.Warn("not enough idle sandboxes to scale down",
			"pool", pool.ID,
			"requested", count,
			"idle", len(candidates))
		count = int64(len(candidates))
	}

	if count == 0 {
		m.log.Debug("no idle sandboxes to retire", "pool", pool.ID)
		return nil
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

	// Stop the first 'count' sandboxes
	stopped := int64(0)
	for i := int64(0); i < count && int(i) < len(candidates); i++ {
		sb := candidates[i].sb

		m.log.Info("retiring idle sandbox",
			"pool", pool.ID,
			"sandbox", sb.ID,
			"last_activity", candidates[i].lastActivity,
			"idle_time", candidates[i].idleTime)

		// Mark sandbox as STOPPED
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sb.ID.String())
		rpcE.SetAttrs(entity.New(
			(&compute_v1alpha.Sandbox{
				Status: compute_v1alpha.STOPPED,
			}).Encode,
		).Attrs())

		if _, err := m.eac.Put(ctx, &rpcE); err != nil {
			m.log.Error("failed to stop sandbox",
				"pool", pool.ID,
				"sandbox", sb.ID,
				"error", err)
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

// updatePoolStatus updates the pool's CurrentInstances and ReadyInstances fields
func (m *Manager) updatePoolStatus(ctx context.Context, pool *compute_v1alpha.SandboxPool, current, ready int64) error {
	// Only update if values changed
	if pool.CurrentInstances == current && pool.ReadyInstances == ready {
		return nil
	}

	pool.CurrentInstances = current
	pool.ReadyInstances = ready

	// Use Patch to update just the status counter fields
	// This explicitly sets the values, even when they are 0 (empty pool)
	attrs := []entity.Attr{
		entity.Ref(entity.DBId, pool.ID),
		entity.Int64(compute_v1alpha.SandboxPoolCurrentInstancesId, current),
		entity.Int64(compute_v1alpha.SandboxPoolReadyInstancesId, ready),
	}

	if _, err := m.eac.Patch(ctx, attrs, 0); err != nil {
		return fmt.Errorf("failed to update pool status: %w", err)
	}

	m.log.Debug("updated pool status",
		"pool", pool.ID,
		"current", current,
		"ready", ready)

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
	for _, sb := range sandboxes {
		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		lastActivity := sb.LastActivity
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

		// Pool is empty, scaled to zero, and idle for over an hour - delete it
		m.log.Info("cleaning up empty pool", "pool", pool.ID, "service", pool.Service, "age", now.Sub(updatedAt))

		if _, err := m.eac.Delete(ctx, pool.ID.String()); err != nil {
			m.log.Error("failed to delete empty pool", "pool", pool.ID, "error", err)
			continue
		}

		m.log.Info("deleted empty pool", "pool", pool.ID)
	}

	return nil
}
