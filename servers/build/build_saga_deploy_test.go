package build

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
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
	return newDeploySagaHarnessWith(t, setActiveVersion)
}

// newDeploySagaHarnessWith is newDeploySagaHarness with a substitute
// set-active-version action, so a test can wedge behaviour into the window
// between the version going live and the deployment record settling.
func newDeploySagaHarnessWith(t *testing.T, setActive any) *sagaTestHarness {
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
		Action(actionSetActiveVer, setActive).Undo(undoSetActiveVersion).
		Action(actionFinalize, finalize).Undo(undoFinalize).
		Action(actionBeginDeploy, beginDeployment).Undo(undoBeginDeployment).
		Action(actionRecordVersion, recordAppVersion).Undo(undoRecordAppVersion).
		Action(actionActivateDeploy, activateDeployment).Undo(undoActivateDeployment).
		RegisterTo(registry)
	require.NoError(t, err, "registering deploy-tracking build saga")

	storage := saga.NewMemoryStorage()
	executor := saga.NewExecutor(
		storage,
		saga.WithRegistry(registry),
		saga.WithLogger(log),
	)

	return &sagaTestHarness{
		t: t, inmem: inmem, builder: builder,
		streams: streams, statuses: statuses,
		registry: registry, executor: executor,
		storage: storage,
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

// A direct-image deploy has no build or push phase to report, but it must still
// carry the server-owned deployment record through activation and release its
// lock. This also exercises the production image branch with BuildKit absent.
func TestBuildSaga_TrackedImage_ActivatesWithoutBuildPhases(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarness(t)
	h.streams.Register("stream-image", makeTar(t, imageOnlyTarball(t)))

	sender := &recordingSender{}
	h.statuses.Register("stream-image", sender)
	t.Cleanup(func() { h.statuses.Unregister("stream-image") })

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-image").
		Input("deploy_cluster_id", "prod").
		WithID("test-tracked-image").
		Execute(ctx)
	require.NoError(t, err)

	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)

	rec := records[0]
	assert.Equal(t, deploylifecycle.StatusActive, rec.Status())
	assert.Equal(t, string(deploylifecycle.PhaseActivating), rec.Deployment.Phase)
	assert.NotEmpty(t, rec.AppVersion(), "the direct-image version must be recorded")

	var phases []string
	for _, deployment := range sender.Deployments {
		phases = append(phases, deployment.Phase)
	}
	assert.Equal(t, []string{
		string(deploylifecycle.PhasePreparing),
		string(deploylifecycle.PhaseActivating),
	}, phases, "a direct-image deploy must not report build or push work")

	blocking, err := h.builder.deploy.Locks().Blocking(ctx, "demo")
	require.NoError(t, err)
	assert.Nil(t, blocking, "an activated direct-image deployment must not keep the lock")
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

// setActiveVersionThenCancel activates the version and then cancels the
// deployment record, standing in for an operator running `miren deploy cancel`
// in the window between the new version going live and the record settling.
// It leaves activate-deployment facing an illegal cancelled -> active
// transition, which is the cheapest deterministic way to fail a settle.
func setActiveVersionThenCancel(ctx context.Context, in setActiveVersionIn) (setActiveVersionOut, error) {
	out, err := setActiveVersion(ctx, in)
	if err != nil {
		return out, err
	}

	deps := saga.Get[*buildSagaDeps](ctx)
	recs, err := deps.builder.deploy.Store().List(ctx, deploylifecycle.Query{
		AppName: in.AppName,
		Status:  deploylifecycle.StatusInProgress,
	})
	if err != nil || len(recs) == 0 {
		return out, err
	}

	return out, deps.builder.deploy.Cancel(ctx, string(recs[0].Deployment.ID), "cancelled mid-deploy")
}

// A deployment record that cannot settle must not roll back a deploy that is
// already live. The record describes the deploy; it does not control it.
//
// Before this was fixed, activate-deployment returned the failed settle to the
// executor, which compensated the saga, and undoSetActiveVersion put the app
// back on the previous version — a bookkeeping write undoing a real deploy.
func TestBuildSaga_Tracked_SettleFailureDoesNotRevertLiveDeploy(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarnessWith(t, setActiveVersionThenCancel)
	h.streams.Register("stream-settle", makeTar(t, dockerfileTarball(t)))

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-settle").
		Input("deploy_cluster_id", "prod").
		WithID("test-settle").
		Execute(ctx)
	require.NoError(t, err, "a failed deployment settle must not fail the build saga")

	appRec, err := h.builder.appClient.GetByName(ctx, "demo")
	require.NoError(t, err)
	assert.NotEmpty(t, appRec.ActiveVersion,
		"the built version must stay live; compensation would have restored the previous one")

	// The cancellation is the real account of what happened, so it stands.
	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, deploylifecycle.StatusCancelled, records[0].Status())

	// And nothing is left holding the deploy lock.
	blocking, err := h.builder.deploy.Locks().Blocking(ctx, "demo")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a failed settle must not strand the deploy lock")
}

// A client that disappears mid-build must not leave the app locked.
//
// A disconnect cancels the saga's context, and the executor's undo loop bails
// on a cancelled context by design, so undoBeginDeployment never runs and the
// deployment record would be left in_progress holding the deploy lock for its
// full TTL. The saga entry point settles it directly for exactly this case,
// mirroring the plain path's deferred failOnError.
func TestBuildSaga_Tracked_ClientDisconnectDoesNotStrandLock(t *testing.T) {
	ctx, disconnect := context.WithCancel(context.Background())

	// Stand in for the client vanishing partway through: cancel the saga's
	// context from inside an action and fail the way a cancelled call would.
	dropped := func(ctx context.Context, in setActiveVersionIn) (setActiveVersionOut, error) {
		disconnect()
		return setActiveVersionOut{}, context.Canceled
	}

	h := newDeploySagaHarnessWith(t, dropped)
	h.streams.Register("stream-drop", makeTar(t, dockerfileTarball(t)))

	sb := &SagaBuilder{
		inner:    h.builder,
		executor: h.executor,
		storage:  h.storage,
		log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	req := &build_v1alpha.DeployRequest{}
	req.SetClusterId("prod")

	err := sb.startBuild(ctx, "test-drop", "demo", "stream-drop", nil, nil, req)
	require.Error(t, err, "a cancelled build must still fail the RPC")

	// Reads below use a live context; the saga's is deliberately dead.
	records, err := h.builder.deploy.Store().List(context.Background(), deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, deploylifecycle.StatusFailed, records[0].Status(),
		"an abandoned deployment must settle rather than linger in_progress")

	blocking, err := h.builder.deploy.Locks().Blocking(context.Background(), "demo")
	require.NoError(t, err)
	assert.Nil(t, blocking, "the next deploy of this app must not wait out the lock TTL")
}

// The saga path must advance the record's phase, not park it at "preparing"
// for the whole build. Other people read that phase: it is the phase column in
// `miren app history` and the "Current phase" line someone sees when their
// deploy is blocked by this one.
//
// buildImage contributes "building" and "pushing", but it is stubbed here (the
// real one needs buildkit, and blackbox covers it), so this asserts the two
// phases the unstubbed actions own.
func TestBuildSaga_Tracked_AdvancesDeploymentPhase(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarness(t)
	h.streams.Register("stream-phase", makeTar(t, dockerfileTarball(t)))

	sender := &recordingSender{}
	h.statuses.Register("stream-phase", sender)
	t.Cleanup(func() { h.statuses.Unregister("stream-phase") })

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-phase").
		Input("deploy_cluster_id", "prod").
		WithID("test-phase").
		Execute(ctx)
	require.NoError(t, err)

	var phases []string
	for _, d := range sender.Deployments {
		phases = append(phases, d.Phase)
	}
	assert.Contains(t, phases, string(deploylifecycle.PhasePreparing))
	assert.Contains(t, phases, string(deploylifecycle.PhaseActivating),
		"the client must be told the deploy reached activation")

	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, string(deploylifecycle.PhaseActivating), records[0].Deployment.Phase,
		"a settled record must not still claim it is preparing")
}
