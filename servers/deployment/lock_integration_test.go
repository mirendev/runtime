package deployment

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func newLockTestClient(t *testing.T) (*deployment_v1alpha.DeploymentClient, *testutils.InMemEntityServer) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	require.NoError(t, err)

	return &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}, inmem
}

// A second CreateDeployment for the same app+cluster must be blocked by the
// lock the first one took, and must return structured lock info.
func TestCreateDeploymentTakesAndReportsLock(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	first, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)
	require.False(t, first.HasError() && first.Error() != "", "first deploy should succeed: %s", first.Error())

	second, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)
	require.True(t, second.HasError() && second.Error() != "", "second deploy must be blocked")
	assert.Contains(t, second.Error(), "blocked")
	require.True(t, second.HasLockInfo() && second.LockInfo() != nil)
	assert.Equal(t, first.Deployment().Id(), second.LockInfo().BlockingDeploymentId())
}

// The lock is app-scoped: a different app is a different lock, but the same app
// with a different client cluster string is still the same lock (a coordinator
// serves one cluster, and the client cluster string is unreliable — MIR-1465).
func TestCreateDeploymentLockIsScopedPerApp(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	_, err := client.CreateDeployment(ctx, "web", "garden", "pending-build", nil)
	require.NoError(t, err)

	otherApp, err := client.CreateDeployment(ctx, "api", "garden", "pending-build", nil)
	require.NoError(t, err)
	assert.False(t, otherApp.HasError() && otherApp.Error() != "", "a different app is a different lock")

	// Same app, a different cluster_id string (the CI/manual split) must still
	// be blocked — otherwise the two would deploy the same app concurrently.
	sameAppOtherClusterString, err := client.CreateDeployment(ctx, "web", "34.122.229.118:8443", "pending-build", nil)
	require.NoError(t, err)
	assert.True(t, sameAppOtherClusterString.HasError() && sameAppOtherClusterString.Error() != "",
		"the same app is a single lock regardless of the cluster string")
	assert.Contains(t, sameAppOtherClusterString.Error(), "blocked")
}

// Settling a deployment through the deprecated status update must release the
// lock, so the next deploy proceeds.
func TestUpdateStatusReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	first, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)
	deploymentID := first.Deployment().Id()

	// A version must be recorded before activation is meaningful; the old CLI
	// did this via UpdateDeploymentAppVersion.
	_, err = client.UpdateDeploymentAppVersion(ctx, deploymentID, "app_version/v1")
	require.NoError(t, err)

	_, err = client.UpdateDeploymentStatus(ctx, deploymentID, "active", "")
	require.NoError(t, err)

	blocked, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, blocked.Held(), "an activated deployment must not keep the lock")

	next, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)
	assert.False(t, next.HasError() && next.Error() != "", "the next deploy must not be blocked")
}

func TestUpdateFailedDeploymentReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	first, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)

	_, err = client.UpdateFailedDeployment(ctx, first.Deployment().Id(), "build broke", "logs")
	require.NoError(t, err)

	blocked, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, blocked.Held(), "a failed deployment must release the lock")
}

func TestCancelDeploymentReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	first, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)

	cancel, err := client.CancelDeployment(ctx, first.Deployment().Id(), "")
	require.NoError(t, err)
	require.True(t, cancel.Success())

	blocked, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, blocked.Held(), "a cancelled deployment must release the lock")
}

func TestGetDeployLockReportsHolder(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	free, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, free.Held(), "no deploy has run, so nothing is held")

	first, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)

	held, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	require.True(t, held.Held())
	require.True(t, held.HasLockInfo() && held.LockInfo() != nil)
	assert.Equal(t, first.Deployment().Id(), held.LockInfo().BlockingDeploymentId())
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
	res, err := client.DeployVersion(ctx, "web", "prod", "web-v1", false, nil, "", "")
	require.NoError(t, err)
	require.False(t, res.HasError() && res.Error() != "", "deploy should succeed: %s", res.Error())

	lock, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.False(t, lock.Held(), "a completed DeployVersion must release the lock")

	// And a subsequent deploy is not blocked.
	res2, err := client.DeployVersion(ctx, "web", "prod", "web-v1", false, nil, "", "")
	require.NoError(t, err)
	assert.False(t, res2.HasError() && res2.Error() != "", "the next deploy must not be blocked")
}

// A DeployVersion whose activation fails must still release the lock, so the
// failure does not block the app's next deploy. SetActiveVersion is made to fail
// by pointing at a version whose app entity does not exist.
func TestDeployVersionFailureReleasesLock(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	// A version with no corresponding app entity: activation will fail.
	_, err := inmem.Client.Create(ctx, "ghost-v1", &core_v1alpha.AppVersion{Version: "ghost-v1"})
	require.NoError(t, err)

	res, err := client.DeployVersion(ctx, "ghost", "prod", "ghost-v1", false, nil, "", "")
	require.NoError(t, err)
	require.True(t, res.HasError() && res.Error() != "", "deploy should have failed to activate")

	// The failure must not have stranded the lock.
	lock, err := client.GetDeployLock(ctx, "ghost", "prod")
	require.NoError(t, err)
	assert.False(t, lock.Held(), "a failed DeployVersion must release the lock")
}

// The "pending-build" placeholder an older client writes must render as empty
// in history, not as the sentinel.
func TestPendingBuildSentinelNormalizedInHistory(t *testing.T) {
	ctx := context.Background()
	client, _ := newLockTestClient(t)

	created, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)
	assert.Equal(t, "", created.Deployment().AppVersionId(),
		"the pending-build placeholder must not surface to clients")

	// And through the list path.
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
