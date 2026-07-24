package build

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
)

// newDeploySagaHarness is newSagaTestHarness plus the deployment lifecycle
// actions and a Builder wired with a tracker, so the server-owned deployment
// record path can be exercised end to end against the in-memory store.
func newDeploySagaHarness(t *testing.T) *sagaTestHarness {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	tempDir := t.TempDir()
	rpcClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
	builder := &Builder{
		Log:        log,
		EAS:        inmem.EAC,
		ec:         entityserver.NewClient(log, inmem.EAC),
		appClient:  app.NewClient(log, rpcClient),
		TempDir:    tempDir,
		cacheLocks: newAppLocks(),
		deploy:     deploylifecycle.NewTracker(log, inmem.EAC),
	}

	streams := NewStreamRegistry(tempDir, log)
	statuses := NewStatusRegistry()

	registry := saga.NewRegistry()
	deps := &buildSagaDeps{builder: builder, streams: streams, statuses: statuses}
	err := saga.Define(sagaBuildFromTar).
		Using(deps).
		Using(log).
		Action(actionReceiveTar, receiveTar).Undo(undoReceiveTar).
		Action(actionLoadSource, loadSource).Undo(undoLoadSource).
		Action(actionGetNextVer, getNextVersion).Undo(undoGetNextVersion).
		Action(actionBuildImage, stubBuildImage).Undo(undoBuildImage).
		Action(actionPrepareConfig, prepareConfig).Undo(undoPrepareConfig).
		Action(actionHandleEphemera, handleEphemeral).Undo(undoHandleEphemeral).
		Action(actionCreateConfigVer, createConfigVersion).Undo(undoCreateConfigVersion).
		Action(actionCreateVersion, createVersion).Undo(undoCreateVersion).
		Action(actionProvisionAddons, provisionAddons).Undo(undoProvisionAddons).
		Action(actionSetActiveVer, setActiveVersion).Undo(undoSetActiveVersion).
		Action(actionFinalize, finalize).Undo(undoFinalize).
		Action(actionBeginDeploy, beginDeployment).Undo(undoBeginDeployment).
		Action(actionRecordVersion, recordAppVersion).Undo(undoRecordAppVersion).
		Action(actionActivateDeploy, activateDeployment).Undo(undoActivateDeployment).
		RegisterTo(registry)
	require.NoError(t, err, "registering deploy-tracking build saga")

	executor := saga.NewExecutor(
		saga.NewMemoryStorage(),
		saga.WithRegistry(registry),
		saga.WithLogger(log),
	)

	return &sagaTestHarness{
		t: t, inmem: inmem, builder: builder,
		streams: streams, statuses: statuses,
		registry: registry, executor: executor,
	}
}

// A build that carries deploy_cluster_id must leave exactly one deployment
// record, active, with its app version recorded — the whole point of the saga
// owning the lifecycle.
func TestBuildSaga_Tracked_CreatesAndActivatesDeployment(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarness(t)
	h.streams.Register("stream-tracked", makeTar(t, dockerfileTarball(t)))

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-tracked").
		Input("deploy_cluster_id", "prod").
		WithID("test-tracked").
		Execute(ctx)
	require.NoError(t, err)

	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1, "exactly one deployment record for a tracked build")

	rec := records[0]
	assert.Equal(t, deploylifecycle.StatusActive, rec.Status())
	assert.NotEmpty(t, rec.AppVersion(), "the built version must be recorded")
	assert.Equal(t, "prod", rec.Deployment.ClusterId)

	// The lock must be free once the deploy settled.
	blocking, err := h.builder.deploy.Locks().Blocking(ctx, "demo")
	require.NoError(t, err)
	assert.Nil(t, blocking, "an activated deployment must not keep the lock")
}

// The deployment ID must reach the client over the status stream so it can
// display and cancel a deployment it did not create.
func TestBuildSaga_Tracked_EmitsDeploymentProgress(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarness(t)
	h.streams.Register("stream-emit", makeTar(t, dockerfileTarball(t)))

	rec := &recordingSender{}
	h.statuses.Register("stream-emit", rec)
	t.Cleanup(func() { h.statuses.Unregister("stream-emit") })

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-emit").
		Input("deploy_cluster_id", "prod").
		WithID("test-emit").
		Execute(ctx)
	require.NoError(t, err)

	require.NotEmpty(t, rec.Deployments, "the deployment arm must be emitted")

	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, string(records[0].Deployment.ID), rec.Deployments[0].DeploymentID)
}

// A build with no deploy_cluster_id (an older client, or ephemeral) must leave
// no server-owned record — the additive-safety guarantee.
func TestBuildSaga_Untracked_CreatesNoDeployment(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarness(t)
	h.streams.Register("stream-untracked", makeTar(t, dockerfileTarball(t)))

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-untracked").
		WithID("test-untracked").
		Execute(ctx)
	require.NoError(t, err)

	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	assert.Empty(t, records, "a build with no DeployRequest must not create a record")
}

// A second tracked build for the same app+cluster, started while the first
// still holds the lock, must fail rather than run alongside it.
func TestBuildSaga_Tracked_SecondBuildBlockedByLock(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarness(t)

	// Take the lock with a standalone deployment that never settles.
	_, err := h.builder.deploy.Begin(ctx, deploylifecycle.BeginParams{AppName: "demo", ClusterID: "prod"})
	require.NoError(t, err)

	h.streams.Register("stream-blocked", makeTar(t, dockerfileTarball(t)))
	err = h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-blocked").
		Input("deploy_cluster_id", "prod").
		WithID("test-blocked").
		Execute(ctx)
	require.Error(t, err, "a build blocked by the lock must fail")
}
