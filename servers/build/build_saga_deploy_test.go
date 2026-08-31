package build

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
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
// set-active-version action, so tests can control the activation window.
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

// A build that carries deploy_cluster_id must leave exactly one successful
// deployment attempt with its app version recorded. The App owns serving state.
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
	assert.Equal(t, deploylifecycle.StatusSucceeded, rec.Status())
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
	assert.Equal(t, deploylifecycle.StatusSucceeded, rec.Status())
	assert.Equal(t, string(deploylifecycle.PhaseActivating), rec.Deployment.Phase)
	assert.NotEmpty(t, rec.AppVersion(), "the direct-image version must be recorded")
	app, _, err := h.builder.deploy.Store().AppByName(ctx, "demo")
	require.NoError(t, err)
	assert.Equal(t, rec.Deployment.ID, app.ActiveDeployment)
	assert.Equal(t, rec.Deployment.Version, app.ActiveVersion)

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

func TestBuildSaga_WithoutClusterIDStillCreatesAttempt(t *testing.T) {
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
	assert.Len(t, records, 1)
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

func TestBuildSaga_CancellationBeforeActivationFailsDeploy(t *testing.T) {
	ctx := context.Background()

	h := newDeploySagaHarnessWith(t, setActiveVersionThenCancel)
	h.streams.Register("stream-settle", makeTar(t, dockerfileTarball(t)))

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-settle").
		Input("deploy_cluster_id", "prod").
		WithID("test-settle").
		Execute(ctx)
	require.Error(t, err)

	appRec, err := h.builder.appClient.GetByName(ctx, "demo")
	require.NoError(t, err)
	assert.Empty(t, appRec.ActiveVersion)

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

// A client that disappears after upload must not stop the server-owned build.
func TestBuildSaga_Tracked_ClientDisconnectStillActivates(t *testing.T) {
	ctx, disconnect := context.WithCancel(context.Background())

	// Stand in for the client vanishing partway through, while the server still
	// has everything it needs to finish.
	dropped := func(ctx context.Context, in setActiveVersionIn) (setActiveVersionOut, error) {
		disconnect()
		return setActiveVersion(ctx, in)
	}

	h := newDeploySagaHarnessWith(t, dropped)
	h.streams.Register("stream-drop", makeTar(t, dockerfileTarball(t)))

	sb := &SagaBuilder{
		inner:    h.builder,
		executor: h.executor,
		log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	req := &build_v1alpha.DeployRequest{}
	req.SetClusterId("prod")

	work := newDeploymentContext(ctx)
	defer work.Close()
	err := sb.startBuild(work.control, work.action, "test-drop", "demo", "stream-drop", nil, nil, req)
	require.NoError(t, err)

	// Reads below use a live context; the saga's is deliberately dead.
	records, err := h.builder.deploy.Store().List(context.Background(), deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, deploylifecycle.StatusSucceeded, records[0].Status())

	blocking, err := h.builder.deploy.Locks().Blocking(context.Background(), "demo")
	require.NoError(t, err)
	assert.Nil(t, blocking)
}

// Cancelling the deployment record is the build's control plane. It stops the
// current action, but the saga's server-owned context remains live long enough
// to persist the failure and compensate everything it already created.
func TestBuildSaga_Tracked_RecordCancellationCompensates(t *testing.T) {
	ctx := context.Background()
	activeStarted := make(chan struct{}, 1)
	waitForCancellation := func(ctx context.Context, _ setActiveVersionIn) (setActiveVersionOut, error) {
		select {
		case activeStarted <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return setActiveVersionOut{}, ctx.Err()
	}

	h := newDeploySagaHarnessWith(t, waitForCancellation)
	h.streams.Register("stream-cancel", makeTar(t, dockerfileTarball(t)))
	sb := &SagaBuilder{
		inner:    h.builder,
		executor: h.executor,
		log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	req := &build_v1alpha.DeployRequest{}
	req.SetClusterId("prod")
	work := newDeploymentContext(ctx)
	defer work.Close()

	done := make(chan error, 1)
	go func() {
		done <- sb.startBuild(work.control, work.action, "test-cancel", "demo", "stream-cancel", nil, nil, req)
	}()
	select {
	case <-activeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not reach the cancellable action")
	}

	records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NoError(t, h.builder.deploy.Cancel(ctx, string(records[0].Deployment.ID), "operator cancelled"))

	select {
	case err := <-done:
		require.Error(t, err)
		remote, ok := errors.AsType[cond.ErrRemote](work.result(err))
		require.True(t, ok)
		assert.Equal(t, "deployment", remote.Category)
		assert.Equal(t, "cancelled", remote.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled build did not finish compensation")
	}

	exec, err := h.storage.Get(ctx, "test-cancel")
	require.NoError(t, err)
	assert.Equal(t, saga.StatusFailed, exec.Status)
	assert.NotNil(t, exec.ExecutedActions[actionCreateVersion].UndoneAt)

	records, err = h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, deploylifecycle.StatusCancelled, records[0].Status())

	blocking, err := h.builder.deploy.Locks().Blocking(ctx, "demo")
	require.NoError(t, err)
	assert.Nil(t, blocking)
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
