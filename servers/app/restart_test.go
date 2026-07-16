package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
)

// TestAppRestart_StopsRunningSandboxes guards MIR-1288/MIR-1410: restart used to
// patch sandbox status with the short-form enum ref entity.Id(STOPPED)
// ("status.stopped") instead of the fully-qualified SandboxStatusStoppedId. The
// ref never resolved, so the sandbox never actually stopped and the command
// reported "0 sandboxes" while doing nothing.
func TestAppRestart_StopsRunningSandboxes(t *testing.T) {
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

	// App with a single web pool holding one RUNNING sandbox.
	appName := "restart-app"
	appID, err := ec.Create(ctx, appName, &core_v1alpha.App{})
	require.NoError(t, err)

	pool := &compute_v1alpha.SandboxPool{App: appID, Service: "web"}
	poolID, err := ec.Create(ctx, "restart-app-pool", pool)
	require.NoError(t, err)

	sb := &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING}
	sbID, err := ec.Create(ctx, "restart-app-sb", sb,
		entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	require.NoError(t, err)

	result, err := client.Restart(ctx, appName, "")
	require.NoError(t, err)
	require.Equal(t, int32(1), result.RestartedPools(), "should restart the one pool")
	require.Equal(t, int32(1), result.StoppedSandboxes(), "should count the stopped sandbox")

	// The real regression guard: the sandbox must actually decode back to
	// STOPPED. With the pre-fix short-form ref it decoded to "" (silent no-op).
	var stopped compute_v1alpha.Sandbox
	require.NoError(t, ec.GetById(ctx, sbID, &stopped))
	require.Equal(t, compute_v1alpha.STOPPED, stopped.Status,
		"sandbox should be STOPPED after restart, not left in a stale/empty status")
}
