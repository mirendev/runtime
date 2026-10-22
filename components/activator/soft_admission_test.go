package activator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// softAdmissionFixture is an auto-mode (requests_per_instance=10) version with
// a real pool entity, so background scale-ups land in the store where the test
// can read them back.
type softAdmissionFixture struct {
	server    *testutils.InMemEntityServer
	activator *localActivator
	ver       *core_v1alpha.AppVersion
	poolID    entity.Id
	key       verKey
	strategy  concurrency.ConcurrencyStrategy
}

func newSoftAdmissionFixture(t *testing.T, desired int64) *softAdmissionFixture {
	t.Helper()
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	appID, err := server.Client.Create(ctx, "test-app", &core_v1alpha.App{})
	require.NoError(t, err)

	ver := &core_v1alpha.AppVersion{
		App:      appID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{{
				Name: "web",
				ServiceConcurrency: core_v1alpha.ServiceConcurrency{
					Mode:                "auto",
					RequestsPerInstance: 10,
				},
			}},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", ver)
	require.NoError(t, err)
	ver.ID = verID

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: ver.ID},
		ReferencedByVersions: []entity.Id{ver.ID},
		DesiredInstances:     desired,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	res, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	strategy := concurrency.NewStrategy(&ver.Config.Services[0].ServiceConcurrency)
	key := verKey{ver.ID.String(), "web"}

	a := &localActivator{
		log:             testutils.TestLogger(t),
		eac:             server.EAC,
		versions:        map[verKey]*versionPoolRef{key: {ver: ver, poolID: poolID, service: "web", strategy: strategy}},
		poolSandboxes:   map[entity.Id]*poolSandboxes{poolID: {pool: pool, service: "web", strategy: strategy}},
		pools:           map[verKey]*poolState{key: {pool: pool, revision: res.Entity().Revision()}},
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	return &softAdmissionFixture{server: server, activator: a, ver: ver, poolID: poolID, key: key, strategy: strategy}
}

// addSandbox tracks a sandbox in the pool with `leases` leases already handed out.
func (f *softAdmissionFixture) addSandbox(t *testing.T, name string, status compute_v1alpha.SandboxStatus, url string, leases int) *sandbox {
	t.Helper()
	tracker := f.strategy.InitializeTracker()
	for range leases {
		tracker.AcquireLease()
	}
	sb := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sandbox/" + name),
			Status: status,
			Spec:   compute_v1alpha.SandboxSpec{Version: f.ver.ID},
		},
		ent:     entity.Blank(),
		url:     url,
		tracker: tracker,
	}
	f.activator.mu.Lock()
	ps := f.activator.poolSandboxes[f.poolID]
	ps.sandboxes = append(ps.sandboxes, sb)
	f.activator.mu.Unlock()
	return sb
}

func (f *softAdmissionFixture) storedDesired(t *testing.T) int64 {
	t.Helper()
	res, err := f.server.EAC.Get(context.Background(), f.poolID.String())
	require.NoError(t, err)
	var pool compute_v1alpha.SandboxPool
	pool.Decode(res.Entity().Entity())
	return pool.DesiredInstances
}

// When every running sandbox is at its requests_per_instance target, the
// request is admitted to the least-loaded one right away instead of waiting
// for a new sandbox, and the pool grows in the background.
func TestAcquireLeaseAdmitsOverTargetAndScales(t *testing.T) {
	f := newSoftAdmissionFixture(t, 2)
	f.addSandbox(t, "busier", compute_v1alpha.RUNNING, "http://busier", 6)
	f.addSandbox(t, "lighter", compute_v1alpha.RUNNING, "http://lighter", 5)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	lease, err := f.activator.AcquireLease(ctx, f.ver, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.Equal(t, "http://lighter", lease.URL, "over-target admission should pick the least-loaded sandbox")
	assert.True(t, lease.Overflow, "a lease admitted over target is marked as overflow")
	assert.Equal(t, 1, lease.Size, "an overflow lease takes one slot, not a batch")

	// 22 slots leased + 2 headroom over 10 per sandbox → 3 sandboxes.
	require.Eventually(t, func() bool { return f.storedDesired(t) == 3 }, 5*time.Second, 20*time.Millisecond)
}

// A burst on a saturated pool asks for enough sandboxes to cover it, rather
// than the single +1 that racing increments used to collapse into.
func TestOverTargetBurstScalesWithDemand(t *testing.T) {
	f := newSoftAdmissionFixture(t, 1)
	f.addSandbox(t, "only", compute_v1alpha.RUNNING, "http://only", 5)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			lease, err := f.activator.AcquireLease(ctx, f.ver, "web")
			assert.NoError(t, err)
			assert.NotNil(t, lease)
		})
	}
	wg.Wait()

	// 10 + 20 single-slot overflow leases = 30, + 2 headroom → ceil(32/10) = 4.
	require.Eventually(t, func() bool { return f.storedDesired(t) == 4 }, 5*time.Second, 20*time.Millisecond)
}

// Waiters parked on a cold pool are all served once a sandbox becomes
// routable, not just the five its target would allow.
func TestParkedWaitersAllServedWhenSandboxBecomesRoutable(t *testing.T) {
	f := newSoftAdmissionFixture(t, 1)
	sb := f.addSandbox(t, "booting", compute_v1alpha.PENDING, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const waiters = 12
	errs := make(chan error, waiters)
	for range waiters {
		go func() {
			lease, err := f.activator.waitForSandbox(ctx, f.ver, "web", false)
			if err == nil && lease == nil {
				err = fmt.Errorf("nil lease")
			}
			errs <- err
		}()
	}

	require.Eventually(t, func() bool {
		f.activator.mu.RLock()
		defer f.activator.mu.RUnlock()
		return len(f.activator.newSandboxChans[f.key]) == waiters
	}, 2*time.Second, 10*time.Millisecond)

	f.activator.mu.Lock()
	sb.sandbox.Status = compute_v1alpha.RUNNING
	sb.url = "http://booted"
	f.activator.mu.Unlock()
	f.activator.wakeAllWaiters()

	for range waiters {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("parked waiter was not served after the sandbox became routable")
		}
	}

	// 12 waiters sized the pool while parked: (12 + 2) / 10 → 2.
	require.Eventually(t, func() bool { return f.storedDesired(t) >= 2 }, 5*time.Second, 20*time.Millisecond)
}

func TestDemandTarget(t *testing.T) {
	f := newSoftAdmissionFixture(t, 1)
	a := f.activator

	target := func() int64 {
		a.mu.RLock()
		defer a.mu.RUnlock()
		return a.demandTargetLocked(f.key)
	}

	assert.Equal(t, int64(1), target(), "an empty pool still wants one sandbox for the request asking")

	f.addSandbox(t, "a", compute_v1alpha.RUNNING, "http://a", 5)
	assert.Equal(t, int64(2), target(), "a full sandbox plus headroom wants a second")

	f.addSandbox(t, "b", compute_v1alpha.PENDING, "", 0)
	assert.Equal(t, int64(2), target(), "pending sandboxes count through DesiredInstances, not demand")

	a.mu.Lock()
	for range 10 {
		a.newSandboxChans[f.key] = append(a.newSandboxChans[f.key], make(chan struct{}, 1))
	}
	a.mu.Unlock()
	assert.Equal(t, int64(3), target(), "parked waiters add a slot each: (10+10+2)/10")

	a.mu.Lock()
	for range 400 {
		a.newSandboxChans[f.key] = append(a.newSandboxChans[f.key], make(chan struct{}, 1))
	}
	a.mu.Unlock()
	assert.Equal(t, int64(concurrency.MaxPoolSize), target(), "demand is capped at the strategy maximum")
}

// A pool created after startup is known only through its sandboxes, not
// a.pools, and over-target admission still has to grow it.
func TestOverTargetScalesPoolNotYetCached(t *testing.T) {
	f := newSoftAdmissionFixture(t, 1)
	f.activator.mu.Lock()
	delete(f.activator.pools, f.key)
	f.activator.mu.Unlock()
	f.addSandbox(t, "only", compute_v1alpha.RUNNING, "http://only", 5)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	lease, err := f.activator.AcquireLease(ctx, f.ver, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.True(t, lease.Overflow, "a lease admitted over target is marked as overflow")

	// 12 slots leased + 2 headroom over 10 per sandbox → 2 sandboxes.
	require.Eventually(t, func() bool { return f.storedDesired(t) == 2 }, 5*time.Second, 20*time.Millisecond)
}

// A re-sync after a watch disconnect refreshes a cached pool whose size
// changed in the store meanwhile. Otherwise a missed scale-down leaves the
// cache claiming more sandboxes than exist, and over-target admission never
// scales back up.
func TestResyncRefreshesStalePoolSize(t *testing.T) {
	f := newSoftAdmissionFixture(t, 1)
	ctx := context.Background()

	f.activator.mu.Lock()
	state := f.activator.pools[f.key]
	stale := *state.pool
	stale.DesiredInstances = 20
	state.pool = &stale
	f.activator.mu.Unlock()

	_, err := f.server.EAC.Patch(ctx, []entity.Attr{
		{ID: entity.DBId, Value: entity.AnyValue(f.poolID)},
		{ID: compute_v1alpha.SandboxPoolDesiredInstancesId, Value: entity.AnyValue(int64(1))},
	}, 0)
	require.NoError(t, err)

	require.NoError(t, f.activator.recoverPools(ctx))

	f.addSandbox(t, "only", compute_v1alpha.RUNNING, "http://only", 5)

	lease, err := f.activator.AcquireLease(ctx, f.ver, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)

	require.Eventually(t, func() bool { return f.storedDesired(t) == 2 }, 5*time.Second, 20*time.Millisecond)
}

// A pool update the watch never delivered can leave the cache claiming more
// sandboxes than exist. Over-target admission re-reads the pool when fewer
// sandboxes are live than the cache claims, and scales from the real size.
func TestOverTargetRereadsPoolWhenCacheOverstatesIt(t *testing.T) {
	f := newSoftAdmissionFixture(t, 1)
	ctx := context.Background()

	f.activator.mu.Lock()
	state := f.activator.pools[f.key]
	stale := *state.pool
	stale.DesiredInstances = 20
	state.pool = &stale
	f.activator.mu.Unlock()

	_, err := f.server.EAC.Patch(ctx, []entity.Attr{
		{ID: entity.DBId, Value: entity.AnyValue(f.poolID)},
		{ID: compute_v1alpha.SandboxPoolDesiredInstancesId, Value: entity.AnyValue(int64(1))},
	}, 0)
	require.NoError(t, err)

	f.addSandbox(t, "only", compute_v1alpha.RUNNING, "http://only", 5)

	lease, err := f.activator.AcquireLease(ctx, f.ver, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)

	require.Eventually(t, func() bool { return f.storedDesired(t) == 2 }, 5*time.Second, 20*time.Millisecond)
}
