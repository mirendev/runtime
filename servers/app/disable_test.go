package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// TestAppDisableEnable walks an app with a fixed worker and an autoscaled web
// service through disable, a refused restart, and enable.
func TestAppDisableEnable(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	appInfo := &AppInfo{
		Log:  slog.Default(),
		EC:   ec,
		CPU:  &metrics.CPUUsage{},
		Mem:  &metrics.MemoryUsage{},
		HTTP: &metrics.HTTPMetrics{},
	}
	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	appName := "disable-app"
	appID, err := ec.Create(ctx, appName, &core_v1alpha.App{})
	require.NoError(t, err)

	verID, err := ec.Create(ctx, "disable-app-v1", &core_v1alpha.AppVersion{
		App:     appID,
		Version: "v1",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{Name: "web", ServiceConcurrency: core_v1alpha.ServiceConcurrency{Mode: "auto", RequestsPerInstance: 10}},
				{Name: "worker", ServiceConcurrency: core_v1alpha.ServiceConcurrency{Mode: "fixed", NumInstances: 2}},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, ec.Patch(ctx, appID, 0, entity.Ref(core_v1alpha.AppActiveVersionId, verID)))

	webPool, err := ec.Create(ctx, "disable-app-web", &compute_v1alpha.SandboxPool{
		App: appID, Service: "web", DesiredInstances: 1, ReferencedByVersions: []entity.Id{verID},
	})
	require.NoError(t, err)
	workerPool, err := ec.Create(ctx, "disable-app-worker", &compute_v1alpha.SandboxPool{
		App: appID, Service: "worker", DesiredInstances: 2, ReferencedByVersions: []entity.Id{verID},
	})
	require.NoError(t, err)

	desired := func(id entity.Id) int64 {
		t.Helper()
		var pool compute_v1alpha.SandboxPool
		require.NoError(t, ec.GetById(ctx, id, &pool))
		return pool.DesiredInstances
	}
	getApp := func() core_v1alpha.App {
		t.Helper()
		var app core_v1alpha.App
		require.NoError(t, ec.GetById(ctx, appID, &app))
		return app
	}

	_, err = client.Enable(ctx, appName)
	require.ErrorContains(t, err, "is not disabled")

	disabled, err := client.Disable(ctx, appName, "moved to the new box")
	require.NoError(t, err)
	assert.Equal(t, int32(2), disabled.ScaledPools())
	assert.Equal(t, int64(0), desired(webPool))
	assert.Equal(t, int64(0), desired(workerPool))
	app := getApp()
	assert.False(t, app.DisabledAt.IsZero())
	assert.Equal(t, "moved to the new box", app.DisabledReason)

	src, err := appInfo.collectAppHealth(ctx)
	require.NoError(t, err)
	for _, entry := range src.apps {
		if entry.name == appName {
			assert.Equal(t, apphealth.Disabled, src.healthOf(entry).Health)
		}
	}

	_, err = client.Restart(ctx, appName, "")
	require.ErrorContains(t, err, "is disabled", "restart would restore fixed counts and undo the disable")

	enabled, err := client.Enable(ctx, appName)
	require.NoError(t, err)
	assert.Equal(t, int32(1), enabled.RestoredPools())
	assert.Equal(t, int64(2), desired(workerPool), "fixed service returns to its configured count")
	assert.Equal(t, int64(0), desired(webPool), "autoscaled service starts on the next request")
	app = getApp()
	assert.True(t, app.DisabledAt.IsZero())
	assert.Empty(t, app.DisabledReason)
}
