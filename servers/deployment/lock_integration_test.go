package deployment

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/rpc"
)

func newLockTestClient(t *testing.T) (*deployment_v1alpha.DeploymentClient, *testutils.InMemEntityServer) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	require.NoError(t, err)
	_, err = inmem.Client.Create(context.Background(), "v1", &core_v1alpha.AppVersion{
		App: "app/web", Version: "v1",
	})
	require.NoError(t, err)

	return &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}, inmem
}

func beginLockTestDeployment(t *testing.T, inmem *testutils.InMemEntityServer, app, cluster string) *deploylifecycle.Record {
	t.Helper()
	tracker := deploylifecycle.NewTracker(slog.Default(), inmem.EAC)
	rec, err := tracker.Begin(context.Background(), deploylifecycle.BeginParams{
		AppName: app, ClusterID: cluster, Operation: deploylifecycle.OperationBuild,
	})
	require.NoError(t, err)
	return rec
}

// Settling a deployment through the deprecated status update must release the
// lock, so the next deploy proceeds.
func TestUpdateStatusReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	first := beginLockTestDeployment(t, inmem, "web", "prod")
	deploymentID := string(first.Deployment.ID)

	// A version must be recorded before activation is meaningful; the old CLI
	// did this via UpdateDeploymentAppVersion.
	_, err := client.UpdateDeploymentAppVersion(ctx, deploymentID, "app_version/v1")
	require.NoError(t, err)

	_, err = client.UpdateDeploymentStatus(ctx, deploymentID, "active", "")
	require.NoError(t, err)

	blocked, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, blocked.Held(), "an activated deployment must not keep the lock")

	beginLockTestDeployment(t, inmem, "web", "prod")
}

func TestUpdateFailedDeploymentReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	first := beginLockTestDeployment(t, inmem, "web", "prod")

	updated, err := client.UpdateFailedDeployment(ctx, string(first.Deployment.ID), "build broke", "logs")
	require.NoError(t, err)
	require.True(t, updated.HasDeployment())
	assert.Equal(t, "build broke", updated.Deployment().ErrorMessage())
	assert.False(t, updated.Deployment().HasBuildLogs(),
		"the deprecated request field must not put embedded logs back into responses")

	blocked, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, blocked.Held(), "a failed deployment must release the lock")
}

func TestCancelDeploymentReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	first := beginLockTestDeployment(t, inmem, "web", "prod")

	cancel, err := client.CancelDeployment(ctx, string(first.Deployment.ID), "")
	require.NoError(t, err)
	require.True(t, cancel.Success())

	blocked, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, blocked.Held(), "a cancelled deployment must release the lock")
}

func TestGetDeployLockReportsHolder(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	free, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, free.Held(), "no deploy has run, so nothing is held")

	first := beginLockTestDeployment(t, inmem, "web", "prod")

	held, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	require.True(t, held.Held())
	require.True(t, held.HasLockInfo() && held.LockInfo() != nil)
	assert.Equal(t, string(first.Deployment.ID), held.LockInfo().BlockingDeploymentId())
}

func TestGetDeployLockValidatesArgs(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	_, err := client.GetDeployLock(ctx, "", "prod")
	require.Error(t, err)

	_, err = client.GetDeployLock(ctx, "web", "")
	require.Error(t, err)
}

// A completed DeployVersion must release the deploy lock it took, so the app is
// not left blocked. Runs two DeployVersions in sequence and checks the lock is
// free between them and that the second is not blocked.
func TestDeployVersionReleasesLockWhenDone(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	// Seed an app and version so DeployVersion can activate it.
	_, err := inmem.Client.Create(ctx, "web", &core_v1alpha.App{})
	require.NoError(t, err)
	_, err = inmem.Client.Create(ctx, "web-v1", &core_v1alpha.AppVersion{Version: "web-v1"})
	require.NoError(t, err)

	// A successful rollback-style deploy must leave the lock free afterward.
	res, err := client.DeployVersion(ctx, "web", "prod", "web-v1", false, nil, "", "", "")
	require.NoError(t, err)
	require.False(t, res.HasError() && res.Error() != "", "deploy should succeed: %s", res.Error())

	lock, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, lock.Held(), "a completed DeployVersion must release the lock")

	// And a subsequent deploy is not blocked.
	res2, err := client.DeployVersion(ctx, "web", "prod", "web-v1", false, nil, "", "", "")
	require.NoError(t, err)
	assert.False(t, res2.HasError() && res2.Error() != "", "the next deploy must not be blocked")
}

// A missing app is rejected before Begin, so it must not publish either lock
// representation as a side effect of validating the request.
func TestDeployVersionMissingAppDoesNotCreateLock(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	// A version with no corresponding app entity fails the preflight lookup.
	_, err := inmem.Client.Create(ctx, "ghost-v1", &core_v1alpha.AppVersion{Version: "ghost-v1"})
	require.NoError(t, err)

	res, err := client.DeployVersion(ctx, "ghost", "prod", "ghost-v1", false, nil, "", "", "")
	require.NoError(t, err)
	require.True(t, res.HasError() && res.Error() != "", "deploy should reject the missing app")

	// Validation must not have created a lock.
	lock, err := client.GetDeployLock(ctx, "ghost", "prod")
	require.NoError(t, err)
	assert.False(t, lock.Held(), "a rejected DeployVersion must not acquire the lock")
}

// The "pending-build" placeholder an older client writes must render as empty
// in history, not as the sentinel.
func TestPendingBuildSentinelNormalizedInHistory(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	_, err := inmem.Client.Create(ctx, "legacy-dep", &core_v1alpha.Deployment{
		AppName: "web", ClusterId: "prod", AppVersion: "pending-build", Status: "in_progress",
	})
	require.NoError(t, err)

	list, err := client.ListDeployments(ctx, "web", "prod", "", 10)
	require.NoError(t, err)
	require.Len(t, list.Deployments(), 1)
	assert.Equal(t, "", list.Deployments()[0].AppVersionId())
}

// A legacy failed-<id> sentinel already in storage must also normalize to empty.
func TestFailedSentinelNormalizedInHistory(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	id, err := inmem.Client.Create(ctx, "legacy-dep", &core_v1alpha.Deployment{
		AppName:   "web",
		ClusterId: "prod",
		Status:    "failed",
	})
	require.NoError(t, err)

	// Rewrite app_version to the legacy failed-<id> sentinel now that we know the id.
	dep := &core_v1alpha.Deployment{
		ID:         id,
		AppName:    "web",
		ClusterId:  "prod",
		Status:     "failed",
		AppVersion: "failed-" + string(id),
	}
	require.NoError(t, inmem.Client.Update(ctx, dep))

	got, err := client.GetDeploymentById(ctx, string(id))
	require.NoError(t, err)
	assert.Equal(t, "", got.Deployment().AppVersionId(),
		"a legacy failed-<id> sentinel must render as empty")
}

// A disabled app is frozen. These paths skip the build, so the build's refusal
// doesn't cover them.
func TestDeployVersionRefusesDisabledApp(t *testing.T) {
	cases := []struct {
		name     string
		rollback bool
		label    string
	}{
		{name: "redeploy"},
		{name: "rollback", rollback: true},
		{name: "preview", label: "feat-x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client, inmem := newLockTestClient(t)

			appID, err := inmem.Client.Create(ctx, "web", &core_v1alpha.App{})
			require.NoError(t, err)
			_, err = inmem.Client.Create(ctx, "web-v1", &core_v1alpha.AppVersion{Version: "web-v1"})
			require.NoError(t, err)

			disabled := core_v1alpha.Disabled{At: time.Now(), Reason: "parked"}
			require.NoError(t, inmem.Client.Patch(ctx, appID, 0,
				entity.Component(core_v1alpha.AppDisabledId, disabled.Encode())))

			ttl := ""
			if tc.label != "" {
				ttl = "48h"
			}
			res, err := client.DeployVersion(ctx, "web", "prod", "web-v1", tc.rollback, nil, tc.label, ttl, "")
			require.NoError(t, err)
			require.True(t, res.HasError(), "a disabled app must refuse this")
			assert.Contains(t, res.Error(), "miren app enable -a web")

			lock, err := client.GetDeployLock(ctx, "web", "prod")
			require.NoError(t, err)
			assert.False(t, lock.Held(), "a refused deploy must not acquire the lock")

			var app core_v1alpha.App
			require.NoError(t, inmem.Client.Get(ctx, "web", &app))
			assert.Empty(t, app.ActiveVersion, "a refused deploy must not change what the app runs")

			if tc.label != "" {
				preview, err := ephemeralx.LookupByLabel(ctx, inmem.EAC, appID, tc.label)
				require.NoError(t, err)
				assert.Nil(t, preview, "a refused preview must not leave a version behind")
			}
		})
	}
}

func TestEnvChangesRefuseDisabledApp(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	appID, err := inmem.Client.Create(ctx, "web", &core_v1alpha.App{})
	require.NoError(t, err)
	disabled := core_v1alpha.Disabled{At: time.Now(), Reason: "parked"}
	require.NoError(t, inmem.Client.Patch(ctx, appID, 0,
		entity.Component(core_v1alpha.AppDisabledId, disabled.Encode())))

	var v deployment_v1alpha.EnvironmentVariable
	v.SetKey("FALLBACK_MODE")
	v.SetValue("1")
	_, err = client.SetEnvVars(ctx, "web", "prod", []*deployment_v1alpha.EnvironmentVariable{&v}, "")
	require.ErrorContains(t, err, "app is disabled")

	_, err = client.DeleteEnvVars(ctx, "web", "prod", []string{"FALLBACK_MODE"}, "")
	require.ErrorContains(t, err, "app is disabled")
}
