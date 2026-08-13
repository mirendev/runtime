package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// At max_concurrent = 1 the guarantee is exact: two runs of the same task
// cannot both be admitted. This is the property users actually depend on --
// "two deploys can't run my migration at the same time".
func TestSlotAdmitsExactlyOne(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	first := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	second := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-2"))

	require.NoError(t, h.reconcile(ctx, first))
	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, first).Status)

	require.NoError(t, h.reconcile(ctx, second))
	assert.Equal(t, run_v1alpha.PENDING, h.getRun(ctx, second).Status,
		"the second run queues rather than being rejected")
	assert.Empty(t, h.getRun(ctx, second).Sandbox, "no sandbox for a run that wasn't admitted")
}

// Once the holder is terminal the slot is reclaimed, so a queued run proceeds
// without anyone having to poke it beyond the ordinary reconcile.
func TestSlotIsReclaimedAfterTheHolderFinishes(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	first := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	second := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-2"))

	require.NoError(t, h.reconcile(ctx, first))
	require.NoError(t, h.reconcile(ctx, second))
	require.Equal(t, run_v1alpha.PENDING, h.getRun(ctx, second).Status)

	h.setSandboxExit(ctx, h.getRun(ctx, first).Sandbox, 0)
	require.NoError(t, h.reconcile(ctx, first))
	require.Equal(t, run_v1alpha.SUCCEEDED, h.getRun(ctx, first).Status)

	require.NoError(t, h.reconcile(ctx, second))
	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, second).Status)
}

// Different tasks have different slots, so a long migration must not block an
// unrelated task on the same app.
func TestSlotsAreScopedPerTask(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	migrate := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))

	other := baseRun(appID, verID, "run/demo-reindex-1")
	other.Task = "reindex"
	reindex := h.createRun(ctx, other)

	require.NoError(t, h.reconcile(ctx, migrate))
	require.NoError(t, h.reconcile(ctx, reindex))

	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, migrate).Status)
	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, reindex).Status)
}

// A slot pointing at a run that no longer exists must be reclaimable. The
// pointer is the validity check precisely so a crashed controller doesn't wedge
// the task until some lease expires.
func TestSlotWithAMissingHolderIsReclaimed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	_, err := h.ctrl.EC.Create(ctx, slotName(appID, "migrate"), &run_v1alpha.RunSlot{
		App:  appID,
		Task: "migrate",
		Run:  entity.Id("run/vanished"),
	})
	require.NoError(t, err)

	id := h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1"))
	require.NoError(t, h.reconcile(ctx, id))

	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, id).Status)
}

// Re-running admission for the run that already holds the slot must succeed.
// The framework drops handler errors without requeueing, so the same step runs
// again routinely.
func TestSlotAdmissionIsIdempotentForTheHolder(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-migrate-1")
	id := h.createRun(ctx, r)

	admitted, err := h.ctrl.admit(ctx, h.getRun(ctx, id))
	require.NoError(t, err)
	require.True(t, admitted)

	admitted, err = h.ctrl.admit(ctx, h.getRun(ctx, id))
	require.NoError(t, err)
	assert.True(t, admitted, "the slot holder is always admitted")
}

// Above one, admission counts live runs. It is explicitly best-effort -- the
// store has no cross-entity compare-and-swap -- but the ceiling must still hold
// under sequential admission, which is what stops runaway automation.
func TestCountingAdmissionHonorsTheCeiling(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	h.setTaskMaxConcurrent(ctx, "migrate", 2)

	ids := []entity.Id{
		h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-1")),
		h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-2")),
		h.createRun(ctx, baseRun(appID, verID, "run/demo-migrate-3")),
	}

	for _, id := range ids {
		require.NoError(t, h.reconcile(ctx, id))
	}

	var running int
	for _, id := range ids {
		if h.getRun(ctx, id).Status == run_v1alpha.RUNNING {
			running++
		}
	}
	assert.Equal(t, 2, running, "the third run waits for a slot to free up")

	// Finishing one lets the queued run through.
	h.setSandboxExit(ctx, h.getRun(ctx, ids[0]).Sandbox, 0)
	require.NoError(t, h.reconcile(ctx, ids[0]))
	require.NoError(t, h.reconcile(ctx, ids[2]))
	assert.Equal(t, run_v1alpha.RUNNING, h.getRun(ctx, ids[2]).Status)
}

// A task that vanished from config while a run was pending must not wedge the
// run; it falls back to the default rather than erroring forever.
func TestMaxConcurrentDefaultsWhenTheTaskIsGone(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	appID, verID := h.seedApp(ctx)

	r := baseRun(appID, verID, "run/demo-gone-1")
	r.Task = "no-longer-declared"
	id := h.createRun(ctx, r)

	n, err := h.ctrl.maxConcurrent(ctx, h.getRun(ctx, id))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestSlotNameFlattensTheAppID(t *testing.T) {
	assert.Equal(t, "slot-app-demo-migrate", slotName(entity.Id("app/demo"), "migrate"))
}
