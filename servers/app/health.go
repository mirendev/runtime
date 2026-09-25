package app

import (
	"context"
	"sort"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
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

// specNeedsNoService reports whether an app declares work or static content but
// no long-running service.
//
// Such an app has no pools, so without this it falls into the same "desired ==
// 0" branch as an autoscaled app that scaled down and reports idle rather than
// its actual steady state.
func specNeedsNoService(spec *core_v1alpha.ConfigSpec) bool {
	if spec == nil {
		return false
	}
	return len(spec.Services) == 0 && (len(spec.Tasks) > 0 || spec.StaticDir != "")
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
	// needsNoService marks an app that has no pools by design, so it must not
	// be read as one that scaled away.
	needsNoService bool
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

// serviceSandboxHealth uses the same pool classifier as app list, while
// retaining the sandbox details needed to explain a failure in app status.
type serviceSandboxHealth struct {
	pool       poolHealth
	running    int32
	dead       int32
	lastExit   time.Time
	lastCode   int64
	hasExit    bool
	lastFailed time.Time
	failedID   string
}

func (r *AppInfo) collectServiceHealth(ctx context.Context, pools []compute_v1alpha.SandboxPool, spec *core_v1alpha.ConfigSpec, now time.Time, hasInstances bool) ([]*app_v1alpha.ServiceHealth, []*app_v1alpha.BoundPort, error) {
	byService := make(map[string]*serviceSandboxHealth)
	byPool := make(map[string]*serviceSandboxHealth)
	for i := range pools {
		pool := &pools[i]
		h := byService[pool.Service]
		if h == nil {
			h = &serviceSandboxHealth{pool: poolHealth{isAutoscale: true}}
			if spec != nil {
				for _, svc := range spec.Services {
					if svc.Name == pool.Service && svc.Concurrency.Mode == "fixed" {
						h.pool.isAutoscale = false
					}
				}
			}
			byService[pool.Service] = h
		}
		h.pool.accumulate(pool, now)
		byPool[pool.ID.String()] = h
	}
	if len(pools) == 0 {
		return nil, nil, nil
	}

	list, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return nil, nil, err
	}
	seenPorts := make(map[int64]bool)
	var boundPorts []*app_v1alpha.BoundPort
	for list.Next() {
		md := list.Metadata()
		if md == nil {
			continue
		}
		poolID, _ := md.Labels.Get("pool")
		h := byPool[poolID]
		if h == nil {
			continue
		}
		var sb compute_v1alpha.Sandbox
		if err := list.Read(&sb); err != nil {
			continue
		}
		switch sb.Status {
		case compute_v1alpha.RUNNING:
			h.running++
		case compute_v1alpha.DEAD:
			h.dead++
			if !sb.Exit.At.IsZero() {
				if !h.hasExit || sb.Exit.At.After(h.lastExit) {
					h.lastCode = sb.Exit.Code
					h.lastExit = sb.Exit.At
					h.hasExit = true
				}
				if sb.Exit.Code != 0 && (h.failedID == "" || sb.Exit.At.After(h.lastFailed)) {
					h.lastFailed = sb.Exit.At
					h.failedID = sb.ID.String()
				}
			}
		case compute_v1alpha.PENDING, compute_v1alpha.NOT_READY, compute_v1alpha.STOPPED:
		}
		// The controller only records bound_port on divergence. Preserve the
		// existing behavior of reporting it for running or booting instances.
		if hasInstances && (sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.PENDING) {
			for _, bp := range sb.BoundPort {
				if bp.Port == 0 || seenPorts[bp.Port] {
					continue
				}
				seenPorts[bp.Port] = true
				var port app_v1alpha.BoundPort
				port.SetPort(bp.Port)
				port.SetAddress(bp.Address)
				boundPorts = append(boundPorts, &port)
			}
		}
	}

	names := make([]string, 0, len(byService))
	for name := range byService {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]*app_v1alpha.ServiceHealth, 0, len(names))
	for _, name := range names {
		h := byService[name]
		var svc app_v1alpha.ServiceHealth
		svc.SetService(name)
		svc.SetHealth(h.pool.classify())
		svc.SetRunning(h.running)
		svc.SetDead(h.dead)
		if h.pool.inCooldown {
			svc.SetCrashCount(h.pool.crashCount)
			svc.SetCooldownSeconds(int32(h.pool.cooldownLeft.Seconds()))
		}
		if h.hasExit {
			svc.SetLastExitCode(h.lastCode)
		}
		if h.failedID != "" {
			svc.SetLastFailureSandbox(h.failedID)
			svc.SetLastFailureAt(standard.ToTimestamp(h.lastFailed))
		}
		result = append(result, &svc)
	}
	return result, boundPorts, nil
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
		if h.needsNoService {
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
