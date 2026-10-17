package deployment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// createCheckedVersion stores a version of app whose web service runs one
// fixed instance from image, with the given health check path (none if empty).
func createCheckedVersion(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, app *core_v1alpha.App, name, image, healthPath string) *core_v1alpha.AppVersion {
	t.Helper()
	cvID, err := server.Client.Create(ctx, name+"-config", &core_v1alpha.ConfigVersion{
		App: app.ID,
		Spec: core_v1alpha.ConfigSpec{
			Services: []core_v1alpha.ConfigSpecServices{{
				Name:        "web",
				Port:        3000,
				HealthCheck: core_v1alpha.ConfigSpecServicesHealthCheck{Path: healthPath},
				Concurrency: core_v1alpha.ConfigSpecServicesConcurrency{Mode: "fixed", NumInstances: 1},
			}},
		},
	})
	require.NoError(t, err)
	ver := &core_v1alpha.AppVersion{
		App:           app.ID,
		ConfigVersion: cvID,
		Version:       name,
		ImageUrl:      image,
	}
	verID, err := server.Client.Create(ctx, name, ver)
	require.NoError(t, err)
	ver.ID = verID
	return ver
}

func activate(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, l *Launcher, app *core_v1alpha.App, ver *core_v1alpha.AppVersion) {
	t.Helper()
	app.ActiveVersion = ver.ID
	require.NoError(t, server.Client.Update(ctx, app))
	require.NoError(t, l.Reconcile(ctx, app, nil))
}

func poolForVersion(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, verID entity.Id) (*compute_v1alpha.SandboxPool, *entity.Entity) {
	t.Helper()
	resp, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	require.NoError(t, err)
	for _, ent := range resp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())
		if pool.SandboxSpec.Version == verID {
			return &pool, ent.Entity()
		}
	}
	t.Fatalf("no pool for version %s", verID)
	return nil, nil
}

func TestHealthCheckKeepsPreviousPoolUntilNewVersionPasses(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	l := newTestLauncher(testutils.TestLogger(t), server.EAC)

	v1 := createCheckedVersion(t, ctx, server, app, "v1", "test:v1", "/up")
	activate(t, ctx, server, l, app, v1)

	v2 := createCheckedVersion(t, ctx, server, app, "v2", "test:v2", "/up")
	activate(t, ctx, server, l, app, v2)

	newPool, newEnt := poolForVersion(t, ctx, server, v2.ID)
	assert.Equal(t, "/up", newPool.SandboxSpec.HealthCheckPath, "the check reaches the sandbox spec")

	// v2 has no sandbox that passed its check, so v1 keeps serving, through
	// the minutely resync too.
	require.NoError(t, l.Reconcile(ctx, app, nil))
	oldPool, _ := poolForVersion(t, ctx, server, v1.ID)
	assert.Equal(t, int64(1), oldPool.DesiredInstances, "previous version keeps running")
	assert.Contains(t, oldPool.ReferencedByVersions, v1.ID)

	// v2 passes: its pool reports a ready sandbox. The next pass retires v1.
	newPool.ReadyInstances = 1
	require.NoError(t, l.updatePool(ctx, &PoolWithEntity{Pool: newPool, Entity: *newEnt}))
	require.NoError(t, l.Reconcile(ctx, app, nil))

	oldPool, _ = poolForVersion(t, ctx, server, v1.ID)
	assert.Equal(t, int64(0), oldPool.DesiredInstances, "previous version retired once the new one passed")
	assert.Empty(t, oldPool.ReferencedByVersions)
}

func TestNoHealthCheckRetiresPreviousPoolAsBefore(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	l := newTestLauncher(testutils.TestLogger(t), server.EAC)

	v1 := createCheckedVersion(t, ctx, server, app, "v1", "test:v1", "")
	activate(t, ctx, server, l, app, v1)
	v2 := createCheckedVersion(t, ctx, server, app, "v2", "test:v2", "")
	activate(t, ctx, server, l, app, v2)

	oldPool, _ := poolForVersion(t, ctx, server, v1.ID)
	assert.Equal(t, int64(0), oldPool.DesiredInstances,
		"without a health check the previous version is retired after the ready wait, as before")
}

func TestHealthCheckThirdDeployRetiresEveryHeldPoolOncePassing(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	l := newTestLauncher(testutils.TestLogger(t), server.EAC)

	v1 := createCheckedVersion(t, ctx, server, app, "v1", "test:v1", "/up")
	activate(t, ctx, server, l, app, v1)
	v2 := createCheckedVersion(t, ctx, server, app, "v2", "test:v2", "/up") // never passes
	activate(t, ctx, server, l, app, v2)
	v3 := createCheckedVersion(t, ctx, server, app, "v3", "test:v3", "/up")
	activate(t, ctx, server, l, app, v3)

	p1, _ := poolForVersion(t, ctx, server, v1.ID)
	assert.Equal(t, int64(1), p1.DesiredInstances, "v1 still serves while v3 boots")

	p3, e3 := poolForVersion(t, ctx, server, v3.ID)
	p3.ReadyInstances = 1
	require.NoError(t, l.updatePool(ctx, &PoolWithEntity{Pool: p3, Entity: *e3}))
	require.NoError(t, l.Reconcile(ctx, app, nil))

	for _, v := range []*core_v1alpha.AppVersion{v1, v2} {
		p, _ := poolForVersion(t, ctx, server, v.ID)
		assert.Equal(t, int64(0), p.DesiredInstances, "%s retired once v3 passed", v.Version)
	}
}
