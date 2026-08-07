package app

import (
	"context"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/entity"
)

// specAllowsScaleToZero reports whether an app's resolved config lets it sit at
// zero instances on purpose. It can't if any service is pinned to a fixed
// instance count. A nil spec defaults to true (autoscale), matching the List
// path's default-autoscale assumption.
func specAllowsScaleToZero(spec *core_v1alpha.ConfigSpec) bool {
	if spec == nil {
		return true
	}
	for _, svc := range spec.Services {
		if svc.Concurrency.Mode == "fixed" {
			return false
		}
	}
	return true
}

// specIsTaskOnly reports whether an app declares work but nothing long-running.
//
// Such an app has no pools, so without this it falls into the same "desired ==
// 0" branch as an autoscaled app that scaled down, and reports idle forever --
// on a code path that means "deliberately asleep" rather than "deployed and
// waiting to be invoked".
func specIsTaskOnly(spec *core_v1alpha.ConfigSpec) bool {
	if spec == nil {
		return false
	}
	return len(spec.Services) == 0 && len(spec.Tasks) > 0
}

// poolHealth aggregates the readiness-relevant fields across an app's (or a
// single version's) sandbox pools. A sandbox only counts toward ReadyInstances
// after it reaches RUNNING, which happens only once it passes the network
// health check, so ready > 0 means at least one instance is actually serving.
type poolHealth struct {
	ready        int
	desired      int
	inCooldown   bool
	crashCount   int64
	cooldownLeft time.Duration
	// isAutoscale defaults to true and is cleared when any contributing pool is
	// configured with fixed concurrency.
	isAutoscale bool
	// isTaskOnly marks an app that declares tasks and no services. It has no
	// pools by design, so it must not be read as one that scaled away.
	isTaskOnly bool
}

// accumulate folds one pool's state into the aggregate.
func (h *poolHealth) accumulate(pool *compute_v1alpha.SandboxPool, now time.Time) {
	h.ready += int(pool.ReadyInstances)
	h.desired += int(pool.DesiredInstances)
	if !pool.CooldownUntil.IsZero() && pool.CooldownUntil.After(now) {
		h.inCooldown = true
		if pool.ConsecutiveCrashCount > h.crashCount {
			h.crashCount = pool.ConsecutiveCrashCount
		}
		if left := pool.CooldownUntil.Sub(now); left > h.cooldownLeft {
			h.cooldownLeft = left
		}
	}
}

// collectBoundPortDivergence scans the sandboxes belonging to the given pools
// and returns the ports they actually bound that diverge from the configured
// port. The sandbox controller records a bound_port component only on
// divergence (MIR-1246), so any bound_port present is by definition a port the
// app chose for itself. Best effort: a listing error just yields no divergence.
func (r *AppInfo) collectBoundPortDivergence(ctx context.Context, poolIDs map[string]bool) []*app_v1alpha.BoundPort {
	if len(poolIDs) == 0 {
		return nil
	}

	sbList, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		r.Log.Warn("failed to list sandboxes for bound-port check", "error", err)
		return nil
	}

	seen := make(map[int64]bool)
	var result []*app_v1alpha.BoundPort

	for sbList.Next() {
		var sb compute_v1alpha.Sandbox
		if err := sbList.Read(&sb); err != nil {
			continue
		}

		md := sbList.Metadata()
		if md == nil {
			continue
		}
		poolLabel, _ := md.Labels.Get("pool")
		if !poolIDs[poolLabel] {
			continue
		}

		// Only living sandboxes describe where the app is serving now.
		if sb.Status != compute_v1alpha.RUNNING && sb.Status != compute_v1alpha.PENDING {
			continue
		}

		for _, bp := range sb.BoundPort {
			if bp.Port == 0 || seen[bp.Port] {
				continue
			}
			seen[bp.Port] = true

			var rbp app_v1alpha.BoundPort
			rbp.SetPort(bp.Port)
			rbp.SetAddress(bp.Address)
			result = append(result, &rbp)
		}
	}

	return result
}

// classify maps the aggregate to a health string. A pool in cooldown is
// crashed regardless of counts; desired == 0 is a deliberately scaled-to-zero
// app rather than a problem.
func (h poolHealth) classify() string {
	switch {
	case h.inCooldown:
		return apphealth.Crashed
	case h.desired == 0:
		// An app with no long-running process has nothing to be idle about: it
		// is deployed and waiting to be invoked, which is its steady state.
		// This must be checked before the autoscale branch, which would
		// otherwise read it as deliberately scaled away.
		if h.isTaskOnly {
			return apphealth.Ready
		}
		// Deliberately scaled to zero only applies to apps that can autoscale
		// down. A fixed service sitting at zero isn't idle, it just isn't up
		// yet, so keep it non-terminal (deploy should keep waiting).
		if h.isAutoscale {
			return apphealth.Idle
		}
		return apphealth.Starting
	case h.ready >= h.desired:
		return apphealth.Healthy
	case h.ready > 0:
		return apphealth.Degraded
	default:
		return apphealth.Starting
	}
}
