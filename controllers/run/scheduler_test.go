package run

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

type schedHarness struct {
	t   *testing.T
	s   *Scheduler
	inm *testutils.InMemEntityServer
}

// newSchedHarness seeds an app whose active version declares one scheduled task.
func newSchedHarness(t *testing.T, schedule string) *schedHarness {
	t.Helper()

	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	appID := entity.Id("app/demo")
	cfgID := entity.Id("config_version/v1-cfg")
	verID := entity.Id("app_version/v1")

	_, err := inm.EAC.Put(ctx, wrap(entity.New(
		(&core_v1alpha.Metadata{Name: "demo"}).Encode,
		entity.DBId, appID,
		(&core_v1alpha.App{ActiveVersion: verID}).Encode,
	), appID))
	require.NoError(t, err)

	_, err = inm.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, cfgID,
		(&core_v1alpha.ConfigVersion{
			App: appID,
			Spec: core_v1alpha.ConfigSpec{
				Tasks: []core_v1alpha.ConfigSpecTasks{
					{Name: "cleanup", Command: "bin/cleanup", Trigger: "schedule", Schedule: schedule, MaxConcurrent: 1},
				},
			},
		}).Encode,
	), cfgID))
	require.NoError(t, err)

	_, err = inm.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, verID,
		(&core_v1alpha.AppVersion{Version: "v1", ImageUrl: "img:v1", ConfigVersion: cfgID}).Encode,
	), verID))
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return &schedHarness{t: t, s: NewScheduler(log, inm.Client, inm.EAC), inm: inm}
}

func (h *schedHarness) runs(ctx context.Context) []run_v1alpha.Run {
	h.t.Helper()

	results, err := h.inm.EAC.List(ctx, entity.Ref(run_v1alpha.RunAppId, entity.Id("app/demo")))
	require.NoError(h.t, err)

	var out []run_v1alpha.Run
	for _, e := range results.Values() {
		var r run_v1alpha.Run
		r.Decode(e.Entity())
		r.ID = entity.Id(e.Id())
		out = append(out, r)
	}
	return out
}

func atUTC(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// A tick that came due while the cluster was down is skipped, not replayed.
// Running an outage's worth of missed cleanups on recovery is a good way to
// turn an outage into an incident.
func TestSchedulerDoesNotBackfillMissedTicks(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	// First sight is well after many ticks have already passed.
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	assert.Empty(t, h.runs(ctx), "the frontier starts at now; history is not replayed")
}

func TestSchedulerFiresATickThatComesDue(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	require.Empty(t, h.runs(ctx))

	// 12:30 comes due.
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:30:01Z")))

	runs := h.runs(ctx)
	require.Len(t, runs, 1)
	assert.Equal(t, "cleanup", runs[0].Task)
	assert.Equal(t, run_v1alpha.SCHEDULE, runs[0].Trigger)
	assert.Equal(t, run_v1alpha.PENDING, runs[0].Status)
	assert.Equal(t, "2026-08-04T12:30:00Z", runs[0].Tick, "the run records the tick it claims")
}

// The dedup guarantee: every replica derives the same tick and therefore the
// same entity name, so repeated sweeps -- or a second replica -- produce one
// run, not several.
func TestSchedulerFiresEachTickExactlyOnce(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))

	// A second scheduler sharing the store, as another replica would be.
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	other := NewScheduler(log, h.inm.Client, h.inm.EAC)
	require.NoError(t, other.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))

	at := atUTC("2026-08-04T12:30:01Z")
	require.NoError(t, h.s.Sweep(ctx, at))
	require.NoError(t, other.Sweep(ctx, at))
	require.NoError(t, h.s.Sweep(ctx, at))

	assert.Len(t, h.runs(ctx), 1, "one tick must produce one run however many replicas evaluate it")
}

// Ticks do not queue. A tick whose predecessor is still going is skipped -- and
// recorded as skipped, because a silent gap is indistinguishable from a job
// that stopped running.
func TestSchedulerRecordsASkippedTick(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:30:01Z")))
	require.Len(t, h.runs(ctx), 1)

	// The first run is still going when the next tick comes due.
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:01Z")))

	runs := h.runs(ctx)
	require.Len(t, runs, 2)

	var skipped int
	for _, r := range runs {
		if r.Status == run_v1alpha.SKIPPED {
			skipped++
			assert.Equal(t, "2026-08-04T13:00:00Z", r.Tick)
		}
	}
	assert.Equal(t, 1, skipped, "the skip has to be visible, not a silent gap")
}

// Once the predecessor finishes, the next tick runs normally.
func TestSchedulerResumesAfterThePredecessorFinishes(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:30:01Z")))

	runs := h.runs(ctx)
	require.Len(t, runs, 1, "the tick should have produced exactly one run")
	first := runs[0]
	_, err := h.inm.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, first.ID),
		(&run_v1alpha.Run{Status: run_v1alpha.SUCCEEDED}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:01Z")))

	var pending int
	for _, r := range h.runs(ctx) {
		if r.Status == run_v1alpha.PENDING {
			pending++
		}
	}
	assert.Equal(t, 1, pending, "a finished predecessor must not hold the schedule back")
}

// An app with no active version has nothing to schedule against.
func TestSchedulerIgnoresAppsWithoutAnActiveVersion(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	_, err := h.inm.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, entity.Id("app/demo")),
		entity.Ref(core_v1alpha.AppActiveVersionId, entity.Id("")),
	).Attrs(), 0)
	require.NoError(t, err)

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:01Z")))
	assert.Empty(t, h.runs(ctx))
}

// Manual and deploy tasks are not on a schedule and must never be fired by it.
func TestSchedulerIgnoresUnscheduledTasks(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-05T12:00:00Z")))
	assert.Empty(t, h.runs(ctx))
}

// The name is what dedups, so it must be a pure function of the tick and stable
// across processes.
func TestTickRunName(t *testing.T) {
	tick := atUTC("2026-08-04T12:30:00Z")

	a := tickRunName("demo", "cleanup", tick)
	assert.Equal(t, a, tickRunName("demo", "cleanup", tick), "same tick, same name")

	assert.NotEqual(t, a, tickRunName("demo", "cleanup", atUTC("2026-08-04T13:00:00Z")))
	assert.NotEqual(t, a, tickRunName("demo", "other", tick))
	assert.NotEqual(t, a, tickRunName("other", "cleanup", tick))

	// Entity names are one segment; an RFC 3339 timestamp is not.
	assert.NotContains(t, a, ":")
	assert.NotContains(t, a, "+")

	// A tick expressed in another zone is the same instant and must not produce
	// a second run.
	tokyo := tick.In(time.FixedZone("JST", 9*60*60))
	assert.Equal(t, a, tickRunName("demo", "cleanup", tokyo))
}

// The frontier is what decides a tick is behind us, so it must only advance
// past ticks that were actually handled. Advancing first would drop any tick
// whose fire failed, permanently.
func TestSchedulerFrontierDoesNotSkipUnfiredTicks(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))

	// Two ticks come due at once.
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:01Z")))

	runs := h.runs(ctx)
	require.Len(t, runs, 2, "both due ticks are accounted for")

	ticks := map[string]bool{}
	for _, r := range runs {
		ticks[r.Tick] = true
	}
	assert.True(t, ticks["2026-08-04T12:30:00Z"], "the earlier tick must not be skipped over")
	assert.True(t, ticks["2026-08-04T13:00:00Z"])
}
