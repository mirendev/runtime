package sandboxpool

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

// Manager reconciles SandboxPool entities by ensuring the actual number of
// sandbox instances matches the desired number specified in the pool.
type Manager struct {
	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
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

// Run starts the reconciliation loop that watches SandboxPool entities
// and reconciles them to match desired state.
func (m *Manager) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		m.log.Info("starting sandbox pool watch")

		_, err := m.eac.WatchIndex(
			ctx,
			entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool),
			stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
				m.log.Debug("received watch event", "op_type", op.OperationType(), "has_entity", op.HasEntity())

				if !op.HasEntity() {
					return nil
				}

				var pool compute_v1alpha.SandboxPool
				pool.Decode(op.Entity().Entity())

				m.log.Info("watch callback triggered for pool", "pool", pool.ID, "service", pool.Service)

				// Trigger reconciliation for this pool
				if err := m.reconcile(ctx, &pool); err != nil {
					m.log.Error("reconcile failed",
						"pool", pool.ID,
						"service", pool.Service,
						"error", err)
				}

				return nil
			}),
		)

		if err != nil && ctx.Err() == nil {
			m.log.Error("watch failed, restarting in 5s", "error", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		return err
	}
}

// reconcile brings the actual sandbox state in line with the desired state
// specified in the pool entity.
func (m *Manager) reconcile(ctx context.Context, pool *compute_v1alpha.SandboxPool) error {
	m.log.Debug("reconciling pool",
		"pool", pool.ID,
		"service", pool.Service,
		"desired", pool.DesiredInstances)

	// Get all sandboxes for this pool
	sandboxes, err := m.listSandboxes(ctx, pool)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	// Count only non-STOPPED sandboxes as "actual" - STOPPED sandboxes are being retired
	actual := int64(0)
	ready := int64(0)
	for _, sb := range sandboxes {
		if sb.Status != compute_v1alpha.STOPPED {
			actual++
		}
		if sb.Status == compute_v1alpha.RUNNING {
			ready++
		}
	}

	desired := pool.DesiredInstances

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
			if sb.Status != compute_v1alpha.STOPPED {
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
			if sb.Status != compute_v1alpha.STOPPED {
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

// listSandboxes returns all sandboxes for a pool
func (m *Manager) listSandboxes(ctx context.Context, pool *compute_v1alpha.SandboxPool) ([]*compute_v1alpha.Sandbox, error) {
	// Query sandboxes by version index (reduces O(N) to O(N_version))
	resp, err := m.eac.List(ctx, entity.Ref(compute_v1alpha.SandboxVersionId, pool.SandboxSpec.Version))
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
		Status:  compute_v1alpha.PENDING,
		Version: pool.SandboxSpec.Version,
		Spec:    pool.SandboxSpec,
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
	// Skip scale-down if ScaleDownDelay is 0 (fixed mode - never retire)
	if pool.ScaleDownDelay == 0 {
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
		if idleTime > pool.ScaleDownDelay {
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

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(pool.ID.String())
	rpcE.SetAttrs(entity.New(
		(&compute_v1alpha.SandboxPool{
			CurrentInstances: current,
			ReadyInstances:   ready,
		}).Encode,
	).Attrs())

	if _, err := m.eac.Put(ctx, &rpcE); err != nil {
		return fmt.Errorf("failed to update pool status: %w", err)
	}

	m.log.Debug("updated pool status",
		"pool", pool.ID,
		"current", current,
		"ready", ready)

	return nil
}
