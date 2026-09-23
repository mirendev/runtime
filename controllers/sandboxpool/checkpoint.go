package sandboxpool

import (
	"context"
	"reflect"
	"slices"
	"time"

	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
)

func checkpointCandidate(pool *compute.SandboxPool, all []*sandboxWithMeta, sb *compute.Sandbox, now time.Time) bool {
	if pool.DesiredInstances != 0 || !slices.Contains(pool.ReferencedByVersions, sb.Spec.Version) ||
		sb.Status != compute.RUNNING || sb.LastActivity.IsZero() || sb.LastActivity.After(now) ||
		!reflect.DeepEqual(sb.Spec, pool.SandboxSpec) || !computeapi.CheckpointEligible(sb) {
		return false
	}
	for _, other := range all {
		if other.sandbox.ID != sb.ID && !computeapi.SandboxDead(other.sandbox.Status) {
			return false
		}
	}
	return true
}

func (m *Manager) autoIdle(ctx context.Context, pool *compute.SandboxPool, lastActivity time.Time) bool {
	resp, err := m.eac.Get(ctx, pool.SandboxSpec.Version.String())
	if err != nil {
		return false
	}
	var ver core_v1alpha.AppVersion
	ver.Decode(resp.Entity().Entity())
	spec, err := coreutil.ResolveRuntimeConfig(ctx, m.eac, &ver)
	if err != nil {
		return false
	}
	svc, err := coreutil.GetServiceConcurrency(spec, pool.Service)
	if err != nil {
		return false
	}
	sc := core_v1alpha.ServiceConcurrency(svc)
	strategy := concurrency.NewStrategyForVersion(&ver, pool.Service, &sc)
	_, auto := strategy.(*concurrency.AutoStrategy)
	return auto && strategy.ScaleDownDelay() > 0 && time.Since(lastActivity) > strategy.ScaleDownDelay()
}

func (m *Manager) reconcileHibernation(ctx context.Context, pool *compute.SandboxPool, all []*sandboxWithMeta) (bool, error) {
	for _, item := range all {
		sb := item.sandbox
		if !computeapi.SandboxHibernation(sb.Status) {
			continue
		}
		// Read the claim's current revision. Never overwrite a runner completing
		// restore, or a deletion racing this pool reconciliation.
		resp, err := m.eac.Get(ctx, sb.ID.String())
		if err != nil {
			return true, err
		}
		var current compute.Sandbox
		current.Decode(resp.Entity().Entity())
		if current.Status != sb.Status {
			return true, nil
		}
		status := sb.Status
		age := time.Since(time.UnixMilli(resp.Entity().UpdatedAt()))
		if status == compute.HIBERNATED && !sb.HibernatedAt.IsZero() {
			age = time.Since(sb.HibernatedAt)
		}
		if !slices.Contains(pool.ReferencedByVersions, sb.Spec.Version) || !reflect.DeepEqual(sb.Spec, pool.SandboxSpec) ||
			(sb.Status == compute.HIBERNATED && age >= computeapi.CheckpointRetention) ||
			(sb.Status != compute.HIBERNATED && age >= PendingSandboxTimeout) {
			status = compute.STOPPED
		} else if sb.Status == compute.HIBERNATED && pool.DesiredInstances > 0 {
			// Only a wake from zero can consume the checkpoint. If another
			// sandbox already occupies the slot, discard rather than clone it.
			status = compute.RESTORING
			if !m.checkpointRunnerReady(ctx, resp.Entity().Entity()) {
				status = compute.STOPPED
			}
			for _, other := range all {
				if other.sandbox.ID != sb.ID && !computeapi.SandboxDead(other.sandbox.Status) {
					status = compute.STOPPED
				}
			}
		}
		if status != sb.Status {
			patch := &compute.Sandbox{Status: status}
			if status == compute.RESTORING {
				patch.RestoredAt = time.Now()
			}
			_, err = m.eac.Patch(ctx, entity.New(entity.DBId, sb.ID,
				patch.Encode).Attrs(), resp.Entity().Revision())
			if err == nil {
				sb.Status = status
			}
			return true, err
		}
		if status == compute.HIBERNATING {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) checkpointRunnerReady(ctx context.Context, ent *entity.Entity) bool {
	var schedule compute.Schedule
	schedule.Decode(ent)
	if schedule.Key.Node == "" {
		return false
	}
	resp, err := m.eac.Get(ctx, schedule.Key.Node.String())
	if err != nil {
		return false
	}
	var node compute.Node
	node.Decode(resp.Entity().Entity())
	return node.Status == compute.READY && node.ApiAddress != ""
}
