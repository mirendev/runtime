package sandbox

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// PoolServer exposes pool inventory and scaling without exposing execution specs.
type PoolServer struct {
	ec *entityserver.Client
}

func NewPoolServer(ec *entityserver.Client) *PoolServer {
	return &PoolServer{ec: ec}
}

var _ compute_v1alpha.SandboxPools = (*PoolServer)(nil)

func (s *PoolServer) List(ctx context.Context, state *compute_v1alpha.SandboxPoolsList) error {
	if _, err := rpc.FilterResource(ctx, rpc.ResourceRequest{Resource: "app", Action: "list"}); err != nil {
		return err
	}
	appName := state.Args().App()
	if appName == "" {
		appName = rpc.BoundApp(ctx)
	}
	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}
	apps, err := inventoryApps(ctx, s.ec)
	if err != nil {
		return err
	}
	// Labels are not indexed, so an app's addon pools can only be found by
	// walking every pool. The filter runs before version lookups, so excluded
	// pools cost no store calls.
	pools, err := s.ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return err
	}
	// Pools for one version (web, worker, ...) share a version and its config,
	// so resolve each version once rather than once per pool.
	versions := make(map[entity.Id]*poolVersion)
	items := make([]*compute_v1alpha.SandboxPoolInfo, 0, pools.Length())
	for pools.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := pools.Read(&pool); err != nil {
			return err
		}
		owner := apps[pool.App]
		allowed, err := filterInventoryApp(ctx, owner)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		ent := pools.Entity()
		app := owner.Instance
		if app == "" {
			// Addon pools name their app only in a label. It identifies the
			// pool but grants nothing, so read it after authorization.
			var md core_v1alpha.Metadata
			md.Decode(ent)
			app, _ = md.Labels.Get("app")
		}
		if appName != "" && app != appName {
			continue
		}
		item := new(compute_v1alpha.SandboxPoolInfo)
		item.SetId(pool.ID.String())
		item.SetShortId(ent.ShortId())
		item.SetApp(app)
		item.SetVersion(pool.SandboxSpec.Version.String())
		item.SetService(pool.Service)
		item.SetScalingMode("auto")
		ver, ok := versions[pool.SandboxSpec.Version]
		if !ok {
			ver = s.poolVersion(ctx, pool.SandboxSpec.Version)
			versions[pool.SandboxSpec.Version] = ver
		}
		if ver != nil && ver.app == pool.App {
			item.SetVersionShortId(ver.shortID)
			if ver.fixed[pool.Service] {
				item.SetScalingMode("fixed")
			}
		}
		item.SetDesiredInstances(pool.DesiredInstances)
		item.SetCurrentInstances(pool.CurrentInstances)
		item.SetReadyInstances(pool.ReadyInstances)
		item.SetCreatedAt(ent.GetCreatedAt().UnixMilli())
		item.SetUpdatedAt(ent.GetUpdatedAt().UnixMilli())
		item.SetRevision(ent.GetRevision())
		items = append(items, item)
	}
	state.Results().SetPools(items)
	return nil
}

// poolVersion is what inventory needs from a pool's version: its owner, short
// ID, and which services run at a fixed scale.
type poolVersion struct {
	app     entity.Id
	shortID string
	fixed   map[string]bool
}

// poolVersion returns nil when the version cannot be read; inventory then
// shows the pool without version details rather than failing the listing.
// Concurrency comes from the stored config alone. Addon bindings only supply
// variables and disks, so resolving them would cost a store round trip and
// load credentials without changing the result.
func (s *PoolServer) poolVersion(ctx context.Context, id entity.Id) *poolVersion {
	if entity.Empty(id) {
		return nil
	}
	var version core_v1alpha.AppVersion
	verEnt, err := s.ec.GetByIdWithEntity(ctx, id, &version)
	if err != nil {
		return nil
	}
	ver := &poolVersion{app: version.App, shortID: verEnt.Entity().ShortId(), fixed: map[string]bool{}}
	if cfg, err := coreutil.ResolveConfig(ctx, s.ec.EAC(), &version); err == nil {
		for _, svc := range cfg.Services {
			if svc.Concurrency.Mode == "fixed" {
				ver.fixed[svc.Name] = true
			}
		}
	}
	return ver
}

// An empty owner denotes an orphan: operators may manage it, but app-bound
// callers must fail the AllowApp check. Store failures are not orphan evidence.
func (s *PoolServer) poolApp(ctx context.Context, pool *compute_v1alpha.SandboxPool) (string, error) {
	var app core_v1alpha.App
	if entity.Empty(pool.App) {
		return "", nil
	}
	ent, err := s.ec.GetByIdWithEntity(ctx, pool.App, &app)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return "", nil
		}
		return "", err
	}
	var md core_v1alpha.Metadata
	md.Decode(ent.Entity())
	kind, ok := ent.Entity().Get(entity.EntityKind)
	if !ok || kind.Value.Id() != core_v1alpha.KindApp || md.Name == "" {
		return "", nil
	}
	return md.Name, nil
}

func (s *PoolServer) SetDesired(ctx context.Context, state *compute_v1alpha.SandboxPoolsSetDesired) error {
	id := state.Args().Pool()
	if id == "" {
		return fmt.Errorf("pool id is required")
	}
	if !state.Args().RawId() && !strings.Contains(id, "/") {
		id = "pool/" + id
	}
	var pool compute_v1alpha.SandboxPool
	ent, err := s.ec.GetByIdWithEntity(ctx, entity.Id(id), &pool)
	if err != nil {
		if rpc.BoundApp(ctx) != "" && errors.Is(err, cond.ErrNotFound{}) {
			return rpc.ErrUnauthorized
		}
		return err
	}
	if kind, ok := ent.Entity().Get(entity.EntityKind); !ok || kind.Value.Id() != compute_v1alpha.KindSandboxPool {
		if rpc.BoundApp(ctx) != "" {
			return rpc.ErrUnauthorized
		}
		return fmt.Errorf("not a sandbox pool: %s", id)
	}
	owner, err := s.poolApp(ctx, &pool)
	if err != nil {
		return err
	}
	if !rpc.AllowApp(ctx, owner) {
		return rpc.ErrUnauthorized
	}
	if expected := state.Args().Revision(); expected != 0 && expected != ent.Revision() {
		return cond.Conflict("sandbox pool", "revision changed")
	}
	count := state.Args().Count()
	if state.Args().Relative() {
		if (count > 0 && pool.DesiredInstances > math.MaxInt64-count) || (count < 0 && pool.DesiredInstances < math.MinInt64-count) {
			return fmt.Errorf("relative adjustment overflows instance count")
		}
		if pool.DesiredInstances+count < 0 {
			return fmt.Errorf("relative adjustment would result in negative count: %d + %d = %d", pool.DesiredInstances, count, pool.DesiredInstances+count)
		}
		count += pool.DesiredInstances
	}
	if count < 0 {
		return fmt.Errorf("count cannot be negative: %d", count)
	}
	if count > concurrency.MaxPoolSize {
		return fmt.Errorf("count exceeds maximum pool size %d: %d", concurrency.MaxPoolSize, count)
	}
	revision := ent.Revision()
	changed := count != pool.DesiredInstances
	if changed {
		// Do not retry relative updates: the caller must observe and resolve
		// a concurrent scaling/controller change rather than silently rebase.
		result, err := s.ec.EAC().Patch(ctx, []entity.Attr{
			entity.Ref(entity.DBId, pool.ID),
			entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, count),
		}, revision)
		if err != nil {
			return fmt.Errorf("failed to patch sandbox pool: %w", err)
		}
		revision = result.Revision()
	}
	state.Results().SetPreviousDesired(pool.DesiredInstances)
	state.Results().SetDesired(count)
	state.Results().SetChanged(changed)
	state.Results().SetRevision(revision)
	state.Results().SetPoolId(pool.ID.String())
	state.Results().SetPoolShortId(ent.Entity().ShortId())
	state.Results().SetService(pool.Service)
	state.Results().SetCurrentInstances(pool.CurrentInstances)
	state.Results().SetReadyInstances(pool.ReadyInstances)
	return nil
}
