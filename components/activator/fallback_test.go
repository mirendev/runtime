package activator

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// fallbackFixture is an activator tracking two versions of one app's web
// service, each with its own pool: v1 (previous) and v2 (active).
type fallbackFixture struct {
	a      *localActivator
	v1, v2 *core_v1alpha.AppVersion
}

func newFallbackFixture() *fallbackFixture {
	f := &fallbackFixture{
		a: &localActivator{
			log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
			versions:        make(map[verKey]*versionPoolRef),
			poolSandboxes:   make(map[entity.Id]*poolSandboxes),
			pools:           make(map[verKey]*poolState),
			newSandboxChans: make(map[verKey][]chan struct{}),
		},
		v1: &core_v1alpha.AppVersion{ID: "ver/v1", App: "app/a", Version: "v1"},
		v2: &core_v1alpha.AppVersion{ID: "ver/v2", App: "app/a", Version: "v2"},
	}
	return f
}

// track registers ver's pool for web with the given health check path and
// sandboxes in the given states (each RUNNING one gets a URL).
func (f *fallbackFixture) track(ver *core_v1alpha.AppVersion, healthPath string, states ...compute_v1alpha.SandboxStatus) {
	poolID := entity.Id("pool/" + ver.Version)
	pool := &compute_v1alpha.SandboxPool{
		ID:          poolID,
		Service:     "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{HealthCheckPath: healthPath},
	}
	ps := &poolSandboxes{pool: pool, service: "web"}
	for i, st := range states {
		sc := &core_v1alpha.ServiceConcurrency{Mode: "auto", RequestsPerInstance: 10}
		sb := &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     entity.Id("sandbox/" + ver.Version + "-" + string(rune('a'+i))),
				Status: st,
				Spec:   compute_v1alpha.SandboxSpec{Version: ver.ID},
			},
			ent:         entity.Blank(),
			lastRenewal: time.Now(),
			tracker:     concurrency.NewStrategy(sc).InitializeTracker(),
		}
		if st == compute_v1alpha.RUNNING {
			sb.url = "http://" + ver.Version + ".internal:3000"
		}
		ps.sandboxes = append(ps.sandboxes, sb)
	}
	key := verKey{ver.ID.String(), "web"}
	f.a.versions[key] = &versionPoolRef{ver: ver, poolID: poolID, service: "web"}
	f.a.poolSandboxes[poolID] = ps
	f.a.pools[key] = &poolState{pool: pool}
}

func TestAcquireLeaseFallsBackToPreviousVersionWhileActiveIsUnhealthy(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)

	lease, err := f.a.AcquireLease(context.Background(), f.v2, "web")
	require.NoError(t, err)
	assert.Equal(t, "http://v1.internal:3000", lease.URL)
	assert.Equal(t, f.v1, lease.Version(), "the lease must name the version it came from")

	// Renewing and releasing find the previous version's pool.
	_, err = f.a.RenewLease(context.Background(), lease)
	require.NoError(t, err)
	require.NoError(t, f.a.ReleaseLease(context.Background(), lease))
}

func TestAcquireLeasePrefersActiveVersionOnceHealthy(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.RUNNING)

	lease, err := f.a.AcquireLease(context.Background(), f.v2, "web")
	require.NoError(t, err)
	assert.Equal(t, "http://v2.internal:3000", lease.URL)
}

func TestFallbackLeaseOnlyForHealthCheckedServices(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "", compute_v1alpha.RUNNING)
	f.track(f.v2, "", compute_v1alpha.PENDING)
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")),
		"without a health check, requests wait for the active version as before")

	// The active version's pool decides: a check added by this deploy counts
	// even though the previous version had none.
	f = newFallbackFixture()
	f.track(f.v1, "", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	assert.NotNil(t, lease(f.a.fallbackLease(f.v2, "web")))
}

func TestFallbackLeaseBeforeActivePoolIsTracked(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	assert.NotNil(t, lease(f.a.fallbackLease(f.v2, "web")),
		"a previous pool with a check stands in until the active pool is seen")

	f = newFallbackFixture()
	f.track(f.v1, "", compute_v1alpha.RUNNING)
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")))
}

func TestFallbackLeaseIgnoresOtherAppsServicesAndPreviews(t *testing.T) {
	f := newFallbackFixture()
	other := &core_v1alpha.AppVersion{ID: "ver/o1", App: "app/other", Version: "o1"}
	f.track(other, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")), "never serve another app's sandbox")
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "worker")), "never serve another service's sandbox")

	f = newFallbackFixture()
	preview := &core_v1alpha.AppVersion{ID: "ver/p1", App: "app/a", Version: "p1", EphemeralLabel: "pr-1"}
	f.track(preview, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")), "never serve a preview's sandbox as production")

	f = newFallbackFixture()
	f.v2.EphemeralLabel = "pr-2"
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")), "a preview never falls back to production")
}

func TestFallbackLeaseSkipsSandboxesThatCannotServe(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.STOPPED, compute_v1alpha.DEAD)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")))
}

func lease(l *Lease, _ bool) *Lease { return l }

func TestFallbackLeaseNotWhileActiveVersionIsRunning(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.RUNNING)
	// v2 passed and is busy: the normal path waits or scales it, rather than
	// splitting traffic back onto v1.
	busy := f.a.poolSandboxes["pool/v2"].sandboxes[0].tracker
	for busy.HasCapacity() {
		busy.AcquireLease()
	}
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")))
}

func TestFallbackLeaseSkipsDiskPools(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	f.a.poolSandboxes["pool/v1"].pool.SandboxSpec.Volume = []compute_v1alpha.SandboxSpecVolume{{Name: "data"}}
	assert.Nil(t, lease(f.a.fallbackLease(f.v2, "web")), "a disk service's old pool is draining")
}

func TestFallbackLeaseKicksAnActivePoolAtZero(t *testing.T) {
	f := newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up") // no sandboxes, DesiredInstances 0

	l, kick := f.a.fallbackLease(f.v2, "web")
	require.NotNil(t, l)
	assert.True(t, kick, "nothing would otherwise start the version the launcher waits on")

	_, kick = f.a.fallbackLease(f.v2, "web")
	assert.False(t, kick, "one request for capacity per interval, not one per request")

	f = newFallbackFixture()
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.PENDING)
	_, kick = f.a.fallbackLease(f.v2, "web")
	assert.False(t, kick, "a booting sandbox needs no kick")
}

func TestFallbackLeasePrefersTheNewestRunningVersion(t *testing.T) {
	// v1 and v2 both still up (v3 deployed before the launcher retired v1):
	// v2 last passed, so it serves. Map order is random, so try repeatedly.
	for i := 0; i < 50; i++ {
		f := newFallbackFixture()
		v3 := &core_v1alpha.AppVersion{ID: "ver/v3", App: "app/a", Version: "v3"}
		f.track(f.v1, "/up", compute_v1alpha.RUNNING)
		f.track(f.v2, "/up", compute_v1alpha.RUNNING)
		f.track(v3, "/up", compute_v1alpha.PENDING)

		l := lease(f.a.fallbackLease(v3, "web"))
		require.NotNil(t, l)
		require.Equal(t, f.v2, l.Version())
	}

	// A newest version with no room leaves the next one to serve.
	f := newFallbackFixture()
	v3 := &core_v1alpha.AppVersion{ID: "ver/v3", App: "app/a", Version: "v3"}
	f.track(f.v1, "/up", compute_v1alpha.RUNNING)
	f.track(f.v2, "/up", compute_v1alpha.RUNNING)
	f.track(v3, "/up", compute_v1alpha.PENDING)
	full := f.a.poolSandboxes["pool/v2"].sandboxes[0].tracker
	for full.HasCapacity() {
		full.AcquireLease()
	}
	assert.Equal(t, f.v1, lease(f.a.fallbackLease(v3, "web")).Version())
}

func TestNewerVersionFollowsCreationOrder(t *testing.T) {
	older := &core_v1alpha.AppVersion{Version: "demo-" + idgen.Gen("v")}
	time.Sleep(2 * time.Millisecond)
	newer := &core_v1alpha.AppVersion{Version: "demo-" + idgen.Gen("v")}

	assert.True(t, newerVersion(newer, older))
	assert.False(t, newerVersion(older, newer))
}
