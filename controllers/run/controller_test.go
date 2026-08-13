package run

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

type harness struct {
	t    *testing.T
	ctrl *Controller
	inm  *testutils.InMemEntityServer
}

// wrap packages attrs for EAC.Put, which takes the RPC entity shape.
func wrap(a *entity.Entity, id entity.Id) *entityserver_v1alpha.Entity {
	var e entityserver_v1alpha.Entity
	e.SetId(id.String())
	e.SetAttrs(a.Attrs())
	return &e
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return &harness{
		t:    t,
		ctrl: NewController(log, inm.Client, inm.EAC),
		inm:  inm,
	}
}

// seedApp creates the app, version, and config a run needs to build a spec.
func (h *harness) seedApp(ctx context.Context) (appID, verID entity.Id) {
	h.t.Helper()

	appID = entity.Id("app/demo")
	_, err := h.inm.EAC.Put(ctx, wrap(entity.New(
		(&core_v1alpha.Metadata{Name: "demo"}).Encode,
		entity.DBId, appID,
		(&core_v1alpha.App{}).Encode,
	), appID))
	require.NoError(h.t, err)

	// Config lives on its own entity, which is what ResolveConfig prefers.
	cfgID := entity.Id("config_version/v1-cfg")
	_, err = h.inm.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, cfgID,
		(&core_v1alpha.ConfigVersion{
			App: appID,
			Spec: core_v1alpha.ConfigSpec{
				StartDirectory: "/app",
				Services:       []core_v1alpha.ConfigSpecServices{{Name: "web", Command: "bin/server"}},
				Tasks: []core_v1alpha.ConfigSpecTasks{
					{Name: "migrate", Command: "bin/rails db:migrate", Trigger: "manual", MaxConcurrent: 1},
				},
			},
		}).Encode,
	), cfgID))
	require.NoError(h.t, err)

	verID = entity.Id("app_version/v1")
	_, err = h.inm.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, verID,
		(&core_v1alpha.AppVersion{
			Version:       "v1",
			ImageUrl:      "registry/demo:v1",
			ConfigVersion: cfgID,
		}).Encode,
	), verID))
	require.NoError(h.t, err)

	return appID, verID
}

func (h *harness) createRun(ctx context.Context, r *run_v1alpha.Run) entity.Id {
	h.t.Helper()

	id := r.ID
	require.NotEmpty(h.t, id, "test must choose the run id")

	_, err := h.inm.EAC.Put(ctx, wrap(entity.New(entity.DBId, id, r.Encode), id))
	require.NoError(h.t, err)
	return id
}

func (h *harness) getRun(ctx context.Context, id entity.Id) *run_v1alpha.Run {
	h.t.Helper()

	resp, err := h.inm.EAC.Get(ctx, id.String())
	require.NoError(h.t, err)

	var r run_v1alpha.Run
	r.Decode(resp.Entity().Entity())
	return &r
}

func (h *harness) reconcile(ctx context.Context, id entity.Id) error {
	h.t.Helper()
	return h.ctrl.Reconcile(ctx, h.getRun(ctx, id), &entity.Meta{})
}

// setSandboxExit puts a sandbox into the state monitorTaskExit leaves behind.
func (h *harness) setSandboxExit(ctx context.Context, id entity.Id, code int64) {
	h.t.Helper()

	_, err := h.inm.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{
			Status: compute.STOPPED,
			Exit:   compute.Exit{Code: code, At: time.Now(), Container: "app"},
		}).Encode,
	).Attrs(), 0)
	require.NoError(h.t, err)
}

// setTaskMaxConcurrent rewrites the seeded config so a task has a different cap.
func (h *harness) setTaskMaxConcurrent(ctx context.Context, task string, n int64) {
	h.t.Helper()

	cfgID := entity.Id("config_version/v1-cfg")
	_, err := h.inm.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, cfgID,
		(&core_v1alpha.ConfigVersion{
			App: entity.Id("app/demo"),
			Spec: core_v1alpha.ConfigSpec{
				StartDirectory: "/app",
				Services:       []core_v1alpha.ConfigSpecServices{{Name: "web", Command: "bin/server"}},
				Tasks: []core_v1alpha.ConfigSpecTasks{
					{Name: task, Command: "bin/rails db:migrate", Trigger: "manual", MaxConcurrent: n},
				},
			},
		}).Encode,
	), cfgID))
	require.NoError(h.t, err)
}

func baseRun(appID, verID entity.Id, id string) *run_v1alpha.Run {
	return &run_v1alpha.Run{
		ID:      entity.Id(id),
		App:     appID,
		Version: verID,
		Task:    "migrate",
		Trigger: run_v1alpha.MANUAL,
		Command: "bin/rails db:migrate",
		Status:  run_v1alpha.PENDING,
	}
}

func TestRunReachesRunningAndCreatesASandbox(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))

	r := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.RUNNING, r.Status)
	assert.NotEmpty(t, r.Sandbox)
	assert.Equal(t, int64(1), r.Attempt)
	assert.False(t, r.StartedAt.IsZero())

	sbResp, err := h.inm.EAC.Get(ctx, r.Sandbox.String())
	require.NoError(t, err)
	var sb compute.Sandbox
	sb.Decode(sbResp.Entity().Entity())

	assert.Equal(t, "bin/rails db:migrate", sb.Spec.Container[0].Command)
	assert.Empty(t, sb.Spec.Container[0].Port, "a run declares no ports")
	assert.True(t, sb.Spec.Container[0].Stdin, "runs are attachable even when nobody is attached")
	assert.Equal(t, compute.SandboxSpecNEVER, sb.Spec.RestartPolicy,
		"a run's command must execute at most once")

	// The run id rides in on the log attributes, which is what makes the
	// sandbox-to-run bridge and `miren logs run` work off one source of truth.
	assert.Equal(t, id, runIDFor(&sb))
}

// Reconciling twice must not create a second sandbox. The framework never
// requeues on error, so a dropped error means the same step runs again.
func TestRunStartIsIdempotent(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))
	first := h.getRun(ctx, id).Sandbox

	// Force it back to pending, as a lost status write would.
	require.NoError(t, h.ctrl.patchRun(ctx, id, &run_v1alpha.Run{Status: run_v1alpha.PENDING}))
	require.NoError(t, h.reconcile(ctx, id))

	assert.Equal(t, first, h.getRun(ctx, id).Sandbox, "the deterministic name makes create idempotent")
}

func TestRunSucceedsOnZeroExit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))

	h.setSandboxExit(ctx, h.getRun(ctx, id).Sandbox, 0)
	require.NoError(t, h.reconcile(ctx, id))

	r := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.SUCCEEDED, r.Status)
	assert.Equal(t, int64(0), r.Result.Code)
	assert.False(t, r.Result.At.IsZero(), "a zero exit code must still be recorded")
	assert.False(t, r.EndedAt.IsZero())
	require.Len(t, r.AttemptRecord, 1)
}

func TestRunFailsOnNonZeroExit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))

	h.setSandboxExit(ctx, h.getRun(ctx, id).Sandbox, 1)
	require.NoError(t, h.reconcile(ctx, id))

	r := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.FAILED, r.Status)
	assert.Equal(t, int64(1), r.Result.Code)
}

// A retry reuses the same entity. For a scheduled run the entity id is derived
// from its tick, and that derivation is the single-execution guarantee -- a
// second entity for attempt two would carry a name that isn't derived from the
// tick and would forfeit it.
func TestRunRetryReusesTheEntityAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-migrate-1")
	r.Trigger = run_v1alpha.SCHEDULE
	r.MaxAttempts = 2
	id := h.createRun(ctx, r)

	require.NoError(t, h.reconcile(ctx, id))
	firstSandbox := h.getRun(ctx, id).Sandbox

	h.setSandboxExit(ctx, firstSandbox, 1)
	require.NoError(t, h.reconcile(ctx, id))

	after := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.PENDING, after.Status, "a retry returns to pending in place")
	assert.Equal(t, int64(2), after.Attempt)
	require.Len(t, after.AttemptRecord, 1, "the failed attempt is kept")
	assert.Equal(t, firstSandbox, after.AttemptRecord[0].Sandbox,
		"the failed attempt's sandbox is the only place its logs live")
	assert.Equal(t, int64(1), after.AttemptRecord[0].ExitCode)

	// Second attempt gets its own sandbox, or create-if-absent would refuse it.
	require.NoError(t, h.reconcile(ctx, id))
	second := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.RUNNING, second.Status)
	assert.NotEqual(t, firstSandbox, second.Sandbox)

	// Exhausting the budget fails for real.
	h.setSandboxExit(ctx, second.Sandbox, 1)
	require.NoError(t, h.reconcile(ctx, id))
	assert.Equal(t, run_v1alpha.FAILED, h.getRun(ctx, id).Status)
}

// Retries exist for triggers nobody is watching. A manual run that fails just
// fails, and the caller decides.
func TestManualRunNeverRetries(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-migrate-1")
	r.MaxAttempts = 5
	id := h.createRun(ctx, r)

	require.NoError(t, h.reconcile(ctx, id))
	h.setSandboxExit(ctx, h.getRun(ctx, id).Sandbox, 1)
	require.NoError(t, h.reconcile(ctx, id))

	assert.Equal(t, run_v1alpha.FAILED, h.getRun(ctx, id).Status)
	assert.Equal(t, int64(1), h.getRun(ctx, id).Attempt)
}

func TestRunCancellation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))
	sandboxID := h.getRun(ctx, id).Sandbox

	// The CLI records a request; the controller owns the status transition.
	require.NoError(t, h.ctrl.patchRun(ctx, id, &run_v1alpha.Run{CancelRequestedAt: time.Now()}))
	require.NoError(t, h.reconcile(ctx, id))

	r := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.CANCELED, r.Status)
	assert.True(t, r.Result.At.IsZero(),
		"no exit was observed, so no exit code should be invented")

	// The sandbox is stopped rather than deleted, so shutdown stays graceful.
	sbResp, err := h.inm.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)
	var sb compute.Sandbox
	sb.Decode(sbResp.Entity().Entity())
	assert.Equal(t, compute.STOPPED, sb.Status)
}

// Cancelling a run that never started should not first require it to be
// admitted and launched.
func TestRunCancellationBeforeStart(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-migrate-1")
	r.CancelRequestedAt = time.Now()
	id := h.createRun(ctx, r)

	require.NoError(t, h.reconcile(ctx, id))

	got := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.CANCELED, got.Status)
	assert.Empty(t, got.Sandbox, "no sandbox should have been created")
}

func TestRunTimesOut(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-migrate-1")
	r.Timeout = "1ms"
	id := h.createRun(ctx, r)

	require.NoError(t, h.reconcile(ctx, id))
	require.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, id).Status)

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, h.reconcile(ctx, id))

	got := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.TIMED_OUT, got.Status)
	assert.True(t, got.Result.At.IsZero(), "a timeout observed no exit code")
}

// timeout = 0 is how a task opts out of being bounded.
func TestRunTimeoutZeroMeansUnbounded(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-migrate-1")
	r.Timeout = "0s"
	id := h.createRun(ctx, r)

	require.NoError(t, h.reconcile(ctx, id))
	running := h.getRun(ctx, id)
	require.Equal(t, run_v1alpha.RUNNING, running.Status)

	// Pretend it started long ago; it must still be running.
	require.NoError(t, h.ctrl.patchRun(ctx, id, &run_v1alpha.Run{
		StartedAt: time.Now().Add(-100 * time.Hour),
	}))
	require.NoError(t, h.reconcile(ctx, id))
	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, id).Status)
}

// A sandbox that stops without reporting an exit -- a runner that went away, or
// one retired by restart policy -- is a failure, but not an exit code.
func TestRunFailsWhenSandboxStopsWithoutAnExit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))

	_, err := h.inm.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, h.getRun(ctx, id).Sandbox),
		(&compute.Sandbox{Status: compute.DEAD}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	require.NoError(t, h.reconcile(ctx, id))

	got := h.getRun(ctx, id)
	assert.Equal(t, run_v1alpha.FAILED, got.Status)
	assert.True(t, got.Result.At.IsZero())
}

func TestRunIDForReadsTheLogAttribute(t *testing.T) {
	sb := &compute.Sandbox{}
	assert.Empty(t, runIDFor(sb))

	sb.Spec.LogAttribute = types.LabelSet("miren.stage", "run", "miren.run", "run/x")
	assert.Equal(t, entity.Id("run/x"), runIDFor(sb))
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []run_v1alpha.RunStatus{
		run_v1alpha.SUCCEEDED, run_v1alpha.FAILED, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED,
	} {
		assert.True(t, isTerminal(s), "%s should be terminal", s)
	}
	for _, s := range []run_v1alpha.RunStatus{"", run_v1alpha.PENDING, run_v1alpha.RUNNING} {
		assert.False(t, isTerminal(s), "%q should not be terminal", s)
	}
}
