package run

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
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

	// Evaluate the same tick from both replicas at once. Sequential sweeps
	// would let the later one observe the earlier one's run and skip on that
	// basis, which proves nothing about the create-if-absent guarantee.
	at := atUTC("2026-08-04T12:30:01Z")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, s := range []*Scheduler{h.s, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Sweep(ctx, at)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err, "losing the race is not an error")
	}
	assert.Len(t, h.runs(ctx), 1, "one tick must produce one run however many replicas evaluate it at once")
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
// past ticks that were actually handled. Advancing first would drop the
// remainder permanently when one fails -- and that difference is invisible
// unless a failure is injected, since a sweep where everything succeeds
// behaves identically either way.
func TestSchedulerFrontierDoesNotSkipUnfiredTicks(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))

	// Two ticks come due at once, and the first one fails to fire.
	real := h.s.fire
	var attempted []time.Time
	h.s.fire = func(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, appName string, task core_v1alpha.ConfigSpecTasks, tick time.Time) error {
		attempted = append(attempted, tick)
		if tick.Equal(atUTC("2026-08-04T12:30:00Z")) {
			return errors.New("injected fire failure")
		}
		return real(ctx, app, ver, appName, task, tick)
	}

	// A failing task is logged rather than aborting the sweep -- one bad task
	// must not stop the others -- so the observable contract is what the next
	// sweep does, not what this one returns.
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:01Z")))
	require.Equal(t, []time.Time{atUTC("2026-08-04T12:30:00Z")}, attempted,
		"the sweep stops at the failed tick rather than racing past it")

	// The failure is over; the next sweep must retry the tick it could not
	// fire, not skip to the newer one.
	h.s.fire = real
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:02Z")))

	ticks := map[string]bool{}
	for _, r := range h.runs(ctx) {
		ticks[r.Tick] = true
	}
	assert.True(t, ticks["2026-08-04T12:30:00Z"],
		"a tick that failed to fire must be retried, not left behind the frontier")
	assert.True(t, ticks["2026-08-04T13:00:00Z"])
}

// A skipped tick is terminal the moment it is created, so it never passes
// through the controller that stamps timestamps. Retention measures from
// EndedAt and retains a scheduled run that has none, so without them the
// busiest task accumulates skipped ticks forever.
func TestSchedulerStampsSkippedTicks(t *testing.T) {
	ctx := context.Background()
	h := newSchedHarness(t, "*-*-* *:00/30:00")

	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:00:00Z")))
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T12:30:01Z")))
	require.NoError(t, h.s.Sweep(ctx, atUTC("2026-08-04T13:00:01Z")))

	var skipped *run_v1alpha.Run
	for _, r := range h.runs(ctx) {
		if r.Status == run_v1alpha.SKIPPED {
			skipped = &r
		}
	}
	require.NotNil(t, skipped, "expected a skipped tick")

	assert.False(t, skipped.EndedAt.IsZero(), "GC retains a scheduled run with no EndedAt forever")
	assert.Equal(t, atUTC("2026-08-04T13:00:00Z"), skipped.EndedAt.UTC(),
		"the tick it claimed is the honest timestamp for a run that never executed")
}

// Stop has to wait for an in-flight sweep, or a caller shutting down in order
// can still see runs created after it returns -- the opposite of what Stop
// appears to promise.
func TestSchedulerStopWaitsForTheSweep(t *testing.T) {
	h := newSchedHarness(t, "*-*-* *:00/30:00")
	h.s.Interval = time.Millisecond

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	real := h.s.fire
	h.s.fire = func(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, appName string, task core_v1alpha.ConfigSpecTasks, tick time.Time) error {
		once.Do(func() { close(entered) })
		<-release
		return real(ctx, app, ver, appName, task, tick)
	}

	// Seed the frontier in the past so the very next sweep has a tick to fire.
	h.s.frontier[entity.Id("app/demo").String()+"/cleanup"] = time.Now().Add(-time.Hour)
	h.s.Start(context.Background())

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep never started")
	}

	stopped := make(chan struct{})
	go func() {
		h.s.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned while a sweep was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after the sweep finished")
	}
}

// The readable prefix joins app and task with a hyphen, and neither excludes
// one -- task names are TOML table keys, so [tasks.db-migrate] is ordinary. A
// run's identity is cluster-wide within its kind, so if the digest covers only
// the tick, these two collide outright whenever their instants agree. Two daily
// schedules agree every day.
//
// The consequence is not a duplicated run but a missing one: the loser of the
// create reads ErrConflict as "another replica claimed this tick", advances its
// frontier, and never retries.
func TestTickRunNameDistinguishesAmbiguousAppAndTaskPairs(t *testing.T) {
	tick := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)

	a := tickRunName("api", "db-migrate", tick)
	b := tickRunName("api-db", "migrate", tick)

	assert.NotEqual(t, a, b, "two different jobs sharing an instant must not share a name")
}

// The dedup property the whole schedule trigger rests on: same app, same task,
// same instant is the same name on every replica.
func TestTickRunNameIsStableForTheSameJobAndTick(t *testing.T) {
	tick := time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)

	assert.Equal(t,
		tickRunName("api", "cleanup", tick),
		tickRunName("api", "cleanup", tick.In(time.FixedZone("elsewhere", 3600))),
		"the same instant in another zone is the same tick")

	assert.NotEqual(t,
		tickRunName("api", "cleanup", tick),
		tickRunName("api", "cleanup", tick.Add(time.Hour)),
		"a different instant is a different tick")
}
