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
				if !op.HasEntity() {
					return nil
				}

				var pool compute_v1alpha.SandboxPool
				pool.Decode(op.Entity().Entity())

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

	// Count actual sandboxes for this pool
	actual, ready, err := m.countSandboxes(ctx, pool)
	if err != nil {
		return fmt.Errorf("failed to count sandboxes: %w", err)
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
		actual, ready, err = m.countSandboxes(ctx, pool)
		if err != nil {
			return fmt.Errorf("failed to count sandboxes after scale up: %w", err)
		}
	}

	// Scale down if needed (TODO: implement with delay + safety checks)
	if actual > desired {
		m.log.Info("scale down needed but not yet implemented",
			"pool", pool.ID,
			"current", actual,
			"desired", desired)
	}

	// Update pool status
	return m.updatePoolStatus(ctx, pool, actual, ready)
}

// countSandboxes returns the total and ready sandbox count for a pool
func (m *Manager) countSandboxes(ctx context.Context, pool *compute_v1alpha.SandboxPool) (total, ready int64, err error) {
	// Query sandboxes by version index (reduces O(N) to O(N_version))
	resp, err := m.eac.List(ctx, entity.Ref(compute_v1alpha.SandboxVersionId, pool.SandboxSpec.Version))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	total = 0
	ready = 0

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Filter by service label (labels not indexed, must filter in-memory)
		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		serviceLabel, _ := md.Labels.Get("service")
		if serviceLabel != pool.Service {
			continue
		}

		total++

		if sb.Status == compute_v1alpha.RUNNING {
			ready++
		}
	}

	return total, ready, nil
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
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name: sbName,
			Labels: types.LabelSet(
				"service", pool.Service,
				"pool", pool.ID.String(),
			),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/"+sbName),
		sb.Encode,
	))

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
	rpcE.SetAttrs(entity.Attrs(
		(&compute_v1alpha.SandboxPool{
			CurrentInstances: current,
			ReadyInstances:   ready,
		}).Encode,
	))

	if _, err := m.eac.Put(ctx, &rpcE); err != nil {
		return fmt.Errorf("failed to update pool status: %w", err)
	}

	m.log.Debug("updated pool status",
		"pool", pool.ID,
		"current", current,
		"ready", ready)

	return nil
}
