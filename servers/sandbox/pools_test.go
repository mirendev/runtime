package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestPoolInventoryAndScaling(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ec := inmem.Client
	client := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(ec))))
	appID, err := ec.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	otherID, err := ec.Create(ctx, "other", &core_v1alpha.App{})
	require.NoError(t, err)
	const secret = "pool-secret-canary"
	cfgID, err := ec.Create(ctx, "shop-config", &core_v1alpha.ConfigVersion{Spec: core_v1alpha.ConfigSpec{
		Services:  []core_v1alpha.ConfigSpecServices{{Name: "web", Concurrency: core_v1alpha.ConfigSpecServicesConcurrency{Mode: "fixed"}}},
		Variables: []core_v1alpha.ConfigSpecVariables{{Key: "USER_SECRET", Value: secret}},
	}})
	require.NoError(t, err)
	versionID, err := ec.Create(ctx, "shop-current", &core_v1alpha.AppVersion{App: appID, ConfigVersion: cfgID})
	require.NoError(t, err)
	oldID, err := ec.Create(ctx, "shop-old", &core_v1alpha.AppVersion{App: appID, Config: core_v1alpha.Config{
		Services: []core_v1alpha.Services{{Name: "worker", ServiceConcurrency: core_v1alpha.ServiceConcurrency{Mode: "fixed"}}},
	}})
	require.NoError(t, err)
	require.NoError(t, ec.UpdateAttrs(ctx, appID, (&core_v1alpha.App{ActiveVersion: versionID}).Encode()))
	_, err = ec.Create(ctx, "shop-addon", &addon_v1alpha.AddonAssociation{
		App: appID, Status: "active", Variables: []addon_v1alpha.Variables{{Key: "DATABASE_URL", Value: secret}},
	})
	require.NoError(t, err)
	poolID, err := ec.Create(ctx, "shop-web", &compute_v1alpha.SandboxPool{
		App: appID, Service: "web", DesiredInstances: 3, CurrentInstances: 2, ReadyInstances: 1,
		SandboxSpec: compute_v1alpha.SandboxSpec{Version: versionID, Container: []compute_v1alpha.SandboxSpecContainer{{Env: []string{"SECRET=" + secret}, Image: "private-image"}}},
	})
	require.NoError(t, err)
	_, err = ec.Create(ctx, "shop-old-worker", &compute_v1alpha.SandboxPool{App: appID, Service: "worker", SandboxSpec: compute_v1alpha.SandboxSpec{Version: oldID}})
	require.NoError(t, err)
	_, err = ec.Create(ctx, "shop-missing-version", &compute_v1alpha.SandboxPool{App: appID, Service: "worker", SandboxSpec: compute_v1alpha.SandboxSpec{Version: "app_version/missing"}})
	require.NoError(t, err)
	otherPool, err := ec.Create(ctx, "other-web", &compute_v1alpha.SandboxPool{App: otherID, DesiredInstances: 8})
	require.NoError(t, err)
	orphan, err := ec.Create(ctx, "orphan", &compute_v1alpha.SandboxPool{App: "app/missing"})
	require.NoError(t, err)

	all, err := client.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, all.Pools(), 5)
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		t.Run(string(method), func(t *testing.T) {
			own := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method, Metadata: map[string]any{"app": "shop", "bound_app": "shop"}})
			for _, filter := range []string{"", "shop"} {
				res, err := client.List(own, filter)
				require.NoError(t, err)
				require.Len(t, res.Pools(), 3, "includes old and missing versions")
				byID := map[string]*compute_v1alpha.SandboxPoolInfo{}
				for _, pool := range res.Pools() {
					require.Equal(t, "shop", pool.App())
					byID[pool.Id()] = pool
					if pool.Version() == oldID.String() {
						require.Equal(t, "fixed", pool.ScalingMode(), "legacy inline config is resolved too")
					} else if pool.Id() != poolID.String() {
						require.Equal(t, "auto", pool.ScalingMode())
					}
				}
				pool := byID[poolID.String()]
				require.NotNil(t, pool)
				require.Equal(t, "fixed", pool.ScalingMode())
				require.Equal(t, versionID.String(), pool.Version())
				require.NotEmpty(t, pool.ShortId())
				require.NotEmpty(t, pool.VersionShortId())
				require.EqualValues(t, 3, pool.DesiredInstances())
				require.EqualValues(t, 2, pool.CurrentInstances())
				require.EqualValues(t, 1, pool.ReadyInstances())
				require.Positive(t, pool.CreatedAt())
				require.Positive(t, pool.UpdatedAt())
				var stored compute_v1alpha.SandboxPool
				ent, err := ec.GetByIdWithEntity(ctx, poolID, &stored)
				require.NoError(t, err)
				require.Equal(t, ent.Revision(), pool.Revision())
				wire, err := json.Marshal(res.Pools())
				require.NoError(t, err)
				for _, excluded := range []string{secret, "private-image", "sandbox_spec", `"env"`, "DATABASE_URL"} {
					require.NotContains(t, string(wire), excluded)
				}
			}
			_, err := client.List(own, "other")
			require.ErrorIs(t, err, rpc.ErrUnauthorized)
			for _, target := range []string{otherPool.String(), versionID.String(), "app_version/missing", orphan.String()} {
				_, err = client.SetDesired(own, target, 0, false, 0, true)
				require.EqualError(t, err, rpc.ErrUnauthorized.Error(), "denials must not disclose owner or entity existence")
			}
			_, err = client.SetDesired(own, poolID.String(), 0, true, 0, false)
			require.NoError(t, err)
			if method == rpc.AuthMethodWorkload {
				res, err := client.SetDesired(own, poolID.String(), 1, true, 0, false)
				require.NoError(t, err)
				require.EqualValues(t, 4, res.Desired())
				_, err = client.SetDesired(own, poolID.String(), -1, true, 0, false)
				require.NoError(t, err)
			}
		})
	}
	_, err = client.SetDesired(ctx, orphan.String(), 1, false, 0, false)
	require.NoError(t, err)
	_, err = client.SetDesired(ctx, versionID.String(), 1, false, 0, true)
	require.ErrorContains(t, err, "not a sandbox pool")

	for _, tc := range []struct {
		count    int64
		relative bool
		want     int64
		changed  bool
	}{
		{5, false, 5, true}, {2, true, 7, true}, {-4, true, 3, true}, {3, false, 3, false}, {0, true, 3, false}, {-3, true, 0, true}, {0, false, 0, false},
	} {
		t.Run(fmt.Sprintf("scale/%d/relative=%t", tc.count, tc.relative), func(t *testing.T) {
			var before compute_v1alpha.SandboxPool
			ent, err := ec.GetByIdWithEntity(ctx, poolID, &before)
			require.NoError(t, err)
			res, err := client.SetDesired(ctx, strings.TrimPrefix(poolID.String(), "pool/"), tc.count, tc.relative, ent.Revision(), false)
			require.NoError(t, err)
			require.Equal(t, poolID.String(), res.PoolId())
			require.Equal(t, ent.Entity().ShortId(), res.PoolShortId())
			require.Equal(t, "web", res.Service())
			require.EqualValues(t, 2, res.CurrentInstances())
			require.EqualValues(t, 1, res.ReadyInstances())
			require.Equal(t, before.DesiredInstances, res.PreviousDesired())
			require.Equal(t, tc.want, res.Desired())
			require.Equal(t, tc.changed, res.Changed())
			var after compute_v1alpha.SandboxPool
			updated, err := ec.GetByIdWithEntity(ctx, poolID, &after)
			require.NoError(t, err)
			require.Equal(t, tc.want, after.DesiredInstances)
			require.Equal(t, before.CurrentInstances, after.CurrentInstances)
			require.Equal(t, before.SandboxSpec, after.SandboxSpec)
			require.Equal(t, updated.Revision(), res.Revision())
			if !tc.changed {
				require.Equal(t, ent.Revision(), updated.Revision())
			}
		})
	}
	_, err = client.SetDesired(ctx, poolID.String(), -1, false, 0, false)
	require.ErrorContains(t, err, "count cannot be negative")
	_, err = client.SetDesired(ctx, poolID.String(), -1, true, 0, false)
	require.ErrorContains(t, err, "relative adjustment would result in negative count")
	_, err = client.SetDesired(ctx, poolID.String(), 3, false, 0, false)
	require.NoError(t, err)

	// Two clients using one inventory revision must not both apply a delta.
	var pool compute_v1alpha.SandboxPool
	ent, err := ec.GetByIdWithEntity(ctx, poolID, &pool)
	require.NoError(t, err)
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := client.SetDesired(ctx, poolID.String(), 2, true, ent.Revision(), false)
			errors <- err
		})
	}
	wg.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		} else {
			require.Contains(t, err.Error(), "conflict")
		}
	}
	require.Equal(t, 1, successes)
	require.NoError(t, ec.GetById(ctx, poolID, &pool))
	require.EqualValues(t, 5, pool.DesiredInstances)
	for _, relative := range []bool{false, true} {
		_, err := client.SetDesired(ctx, poolID.String(), 5, relative, ent.Revision(), false)
		require.ErrorContains(t, err, "conflict")
	}
	var other compute_v1alpha.SandboxPool
	require.NoError(t, ec.GetById(ctx, otherPool, &other))
	require.EqualValues(t, 8, other.DesiredInstances)
}

func TestPoolScalingLimits(t *testing.T) {
	ctx := t.Context()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	client := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(inmem.Client))))
	for _, tc := range []struct {
		name     string
		initial  int64
		count    int64
		relative bool
		want     int64
		err      string
	}{
		{"absolute-limit", 3, 20, false, 20, ""},
		{"absolute-above-limit", 3, 21, false, 0, "exceeds maximum pool size 20"},
		{"relative-limit", 3, 17, true, 20, ""},
		{"relative-above-limit", 3, 18, true, 0, "exceeds maximum pool size 20"},
		{"large-relative", 3, 50, true, 0, "exceeds maximum pool size 20"},
		{"relative-noop-at-limit", 20, 0, true, 20, ""},
		{"repair-legacy-count", 500, -480, true, 20, ""},
		{"still-above-limit", 500, -1, true, 0, "exceeds maximum pool size 20"},
		{"positive-overflow", 3, math.MaxInt64, true, 0, "overflows"},
		{"minimum-delta", 3, math.MinInt64, true, 0, "relative adjustment would result in negative count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := inmem.Client.Create(ctx, tc.name, &compute_v1alpha.SandboxPool{DesiredInstances: tc.initial})
			require.NoError(t, err)
			var pool compute_v1alpha.SandboxPool
			before, err := inmem.Client.GetByIdWithEntity(ctx, id, &pool)
			require.NoError(t, err)
			res, err := client.SetDesired(ctx, id.String(), tc.count, tc.relative, before.Revision(), true)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, res.Desired())
			}
			after, err := inmem.Client.GetByIdWithEntity(ctx, id, &pool)
			require.NoError(t, err)
			if tc.err != "" || tc.initial == tc.want {
				require.Equal(t, tc.initial, pool.DesiredInstances)
				require.Equal(t, before.Revision(), after.Revision())
			} else {
				require.Equal(t, tc.want, pool.DesiredInstances)
			}
		})
	}
}

func TestPoolOrphanScaling(t *testing.T) {
	ctx := t.Context()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	nameless := entity.Id("app/nameless")
	inmem.AddEntity(entity.New(entity.DBId, nameless, (&core_v1alpha.App{}).Encode))
	wrongKind, err := inmem.Client.Create(ctx, "not-an-app", &core_v1alpha.AppVersion{})
	require.NoError(t, err)
	client := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(inmem.Client))))
	for i, owner := range []entity.Id{"", "app/missing", nameless, wrongKind} {
		t.Run(owner.String(), func(t *testing.T) {
			id, err := inmem.Client.Create(ctx, fmt.Sprintf("orphan-%d", i), &compute_v1alpha.SandboxPool{App: owner, DesiredInstances: 4})
			require.NoError(t, err)
			listed, err := client.List(ctx, "")
			require.NoError(t, err)
			require.Len(t, listed.Pools(), 1)
			require.Empty(t, listed.Pools()[0].App())
			for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
				bound := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method, Metadata: map[string]any{"app": "shop", "bound_app": "shop"}})
				_, err = client.SetDesired(bound, id.String(), 0, false, 0, true)
				require.EqualError(t, err, rpc.ErrUnauthorized.Error())
			}
			for _, method := range []rpc.AuthMethod{rpc.AuthMethodCert, rpc.AuthMethodJWT} {
				operator := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method, Subject: "operator"})
				res, err := client.SetDesired(operator, id.String(), -4, true, 0, true)
				require.NoError(t, err)
				require.EqualValues(t, 0, res.Desired())
				var stored compute_v1alpha.SandboxPool
				require.NoError(t, inmem.Client.GetById(ctx, id, &stored))
				require.Zero(t, stored.DesiredInstances)
				res, err = client.SetDesired(operator, id.String(), 4, false, res.Revision(), true)
				require.NoError(t, err)
				require.EqualValues(t, 4, res.Desired())
			}
			require.NoError(t, inmem.Client.Delete(ctx, id))
		})
	}
}

type poolOwnerFailureStore struct {
	entity.Store
	owner entity.Id
}

func (s poolOwnerFailureStore) GetEntity(ctx context.Context, id entity.Id) (*entity.Entity, error) {
	if id == s.owner {
		return nil, cond.Corruption("entity", "owner store unavailable")
	}
	return s.Store.GetEntity(ctx, id)
}

func (s poolOwnerFailureStore) ListIndexRevision(context.Context, entity.Attr) ([]entity.Id, int64, error) {
	return nil, 0, cond.Corruption("entity", "owner store unavailable")
}

func TestPoolOwnerStoreFailure(t *testing.T) {
	ctx := t.Context()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	id, err := inmem.Client.Create(ctx, "shop-web", &compute_v1alpha.SandboxPool{App: appID, DesiredInstances: 4})
	require.NoError(t, err)
	inmem.Server.Store = poolOwnerFailureStore{Store: inmem.Store, owner: appID}
	client := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(inmem.Client))))
	bound := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "shop"}})
	for _, caller := range []context.Context{ctx, bound} {
		_, err = client.List(caller, "")
		require.ErrorIs(t, err, cond.ErrCorruption{})
		_, err = client.SetDesired(caller, id.String(), 0, false, 0, true)
		require.ErrorIs(t, err, cond.ErrCorruption{})
	}
	var stored compute_v1alpha.SandboxPool
	require.NoError(t, inmem.Client.GetById(ctx, id, &stored))
	require.EqualValues(t, 4, stored.DesiredInstances)
}

// Hold both writes until both handlers have read the pool. This exercises the
// read/patch race even when clients do not supply an inventory revision.
type poolPatchBarrier struct {
	rpc.Client
	ready sync.WaitGroup
}

func (c *poolPatchBarrier) Call(ctx context.Context, method string, args, result any) error {
	if method == "patch" {
		c.ready.Done()
		c.ready.Wait()
	}
	return c.Client.Call(ctx, method, args, result)
}

func TestPoolScalingConcurrentReadPatch(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	poolID, err := inmem.Client.Create(ctx, "shop-web", &compute_v1alpha.SandboxPool{App: appID, DesiredInstances: 3})
	require.NoError(t, err)
	barrier := &poolPatchBarrier{Client: inmem.EAC.Client}
	barrier.ready.Add(2)
	ec := entityserver.NewClient(testutils.TestLogger(t), entityserver_v1alpha.NewEntityAccessClient(barrier))
	client := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(ec))))
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := client.SetDesired(ctx, poolID.String(), 2, true, 0, false)
			errors <- err
		}()
	}
	first, second := <-errors, <-errors
	require.NotEqual(t, first == nil, second == nil, "exactly one update must succeed")
	if first != nil {
		require.ErrorContains(t, first, "conflict")
	} else {
		require.ErrorContains(t, second, "conflict")
	}
	var pool compute_v1alpha.SandboxPool
	require.NoError(t, inmem.Client.GetById(ctx, poolID, &pool))
	require.EqualValues(t, 5, pool.DesiredInstances)
}
