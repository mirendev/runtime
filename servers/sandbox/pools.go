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
	index := entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool)
	if appName != "" {
		var appID entity.Id
		for id, owner := range apps {
			if owner.Instance == appName {
				appID = id
				break
			}
		}
		if entity.Empty(appID) {
			state.Results().SetPools([]*compute_v1alpha.SandboxPoolInfo{})
			return nil
		}
		allowed, err := filterInventoryApp(ctx, apps[appID])
		if err != nil {
			return err
		}
		if !allowed {
			state.Results().SetPools([]*compute_v1alpha.SandboxPoolInfo{})
			return nil
		}
		index = entity.Ref(compute_v1alpha.SandboxPoolAppId, appID)
	}
	pools, err := s.ec.List(ctx, index)
	if err != nil {
		return err
	}
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
		item := new(compute_v1alpha.SandboxPoolInfo)
		item.SetId(pool.ID.String())
		item.SetShortId(ent.ShortId())
		item.SetApp(owner.Instance)
		item.SetVersion(pool.SandboxSpec.Version.String())
		item.SetService(pool.Service)
		item.SetScalingMode("auto")
		var version core_v1alpha.AppVersion
		if verEnt, err := s.ec.GetByIdWithEntity(ctx, pool.SandboxSpec.Version, &version); err == nil && version.App == pool.App {
			item.SetVersionShortId(verEnt.Entity().ShortId())
			if cfg, err := coreutil.ResolveRuntimeConfig(ctx, s.ec.EAC(), &version); err == nil {
				for _, svc := range cfg.Services {
					if svc.Name == pool.Service && svc.Concurrency.Mode == "fixed" {
						item.SetScalingMode("fixed")
						break
					}
				}
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
