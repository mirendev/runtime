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
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

type gcHarness struct {
	t   *testing.T
	gc  *GCController
	inm *testutils.InMemEntityServer
	app entity.Id
}

func newGCHarness(t *testing.T) *gcHarness {
	t.Helper()

	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	appID := entity.Id("app/demo")
	_, err := inm.EAC.Put(context.Background(), wrap(entity.New(
		(&core_v1alpha.Metadata{Name: "demo"}).Encode,
		entity.DBId, appID,
		(&core_v1alpha.App{}).Encode,
	), appID))
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return &gcHarness{t: t, gc: NewGCController(log, inm.Client, inm.EAC), inm: inm, app: appID}
}

func (h *gcHarness) put(id string, r *run_v1alpha.Run) entity.Id {
	h.t.Helper()

	r.App = h.app
	eid := entity.Id(id)
	_, err := h.inm.EAC.Put(context.Background(), wrap(entity.New(entity.DBId, eid, r.Encode), eid))
	require.NoError(h.t, err)
	return eid
}

func (h *gcHarness) exists(id entity.Id) bool {
	h.t.Helper()
	_, err := h.inm.EAC.Get(context.Background(), id.String())
	return err == nil
}

// A run still going is never touched, whatever the retention says.
func TestGCNeverDeletesANonTerminalRun(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)

	long := h.put("run/still-going", &run_v1alpha.Run{
		Task: "reindex", Trigger: run_v1alpha.MANUAL,
		Status: run_v1alpha.RUNNING, StartedAt: time.Now().Add(-365 * 24 * time.Hour),
	})

	_, err := h.gc.RunGC(ctx, time.Now())
	require.NoError(t, err)
	assert.True(t, h.exists(long))
}

// The dedup guard for a scheduled tick is the run entity's existence, so a
// scheduled run must survive the count cap entirely -- otherwise a busy app
// evicts same-day ticks and the job silently double-fires.
func TestGCExemptsScheduledRunsFromTheCountCap(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)
	h.gc.Config.RetentionCount = 2
	h.gc.Config.RetentionPeriod = 0

	now := time.Now()
	var ids []entity.Id
	for i := range 10 {
		ids = append(ids, h.put("run/tick-"+string(rune('a'+i)), &run_v1alpha.Run{
			Task: "cleanup", Trigger: run_v1alpha.SCHEDULE, Status: run_v1alpha.SUCCEEDED,
			EndedAt: now.Add(-time.Duration(i) * time.Hour),
			Tick:    now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
		}))
	}

	_, err := h.gc.RunGC(ctx, now)
	require.NoError(t, err)

	for _, id := range ids {
		assert.True(t, h.exists(id), "%s: a scheduled run inside the floor must survive the count cap", id)
	}
}

// Past the floor they do go, or scheduled runs would accumulate forever.
func TestGCRetiresScheduledRunsPastTheFloor(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)
	h.gc.Config.ScheduledFloor = 24 * time.Hour

	now := time.Now()
	recent := h.put("run/tick-recent", &run_v1alpha.Run{
		Task: "cleanup", Trigger: run_v1alpha.SCHEDULE, Status: run_v1alpha.SUCCEEDED,
		EndedAt: now.Add(-1 * time.Hour),
	})
	ancient := h.put("run/tick-ancient", &run_v1alpha.Run{
		Task: "cleanup", Trigger: run_v1alpha.SCHEDULE, Status: run_v1alpha.SUCCEEDED,
		EndedAt: now.Add(-72 * time.Hour),
	})

	_, err := h.gc.RunGC(ctx, now)
	require.NoError(t, err)

	assert.True(t, h.exists(recent), "inside the floor")
	assert.False(t, h.exists(ancient), "past the floor")
}

// Console runs are an audit record and a scrollback, not a permanent log, so
// they get a shorter window than everything else.
func TestGCRetiresConsoleRunsSooner(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)
	h.gc.Config.ConsoleRetention = time.Hour
	h.gc.Config.RetentionPeriod = 30 * 24 * time.Hour

	now := time.Now()
	console := h.put("run/console-old", &run_v1alpha.Run{
		Task: "console", Trigger: run_v1alpha.MANUAL, Status: run_v1alpha.SUCCEEDED,
		EndedAt: now.Add(-2 * time.Hour),
	})
	other := h.put("run/reindex-old", &run_v1alpha.Run{
		Task: "reindex", Trigger: run_v1alpha.MANUAL, Status: run_v1alpha.SUCCEEDED,
		EndedAt: now.Add(-2 * time.Hour),
	})

	_, err := h.gc.RunGC(ctx, now)
	require.NoError(t, err)

	assert.False(t, h.exists(console), "console runs age out quickly")
	assert.True(t, h.exists(other), "an ordinary run of the same age is kept")
}

// Count and age both have to be satisfied: the count alone would evict a burst
// of recent runs, the age alone would let a busy app grow without bound.
func TestGCAppliesCountAndAgeTogether(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)
	h.gc.Config.RetentionCount = 2
	h.gc.Config.RetentionPeriod = time.Hour

	now := time.Now()
	// Three recent runs: over the count, but inside the age floor.
	var recent []entity.Id
	for i := range 3 {
		recent = append(recent, h.put("run/recent-"+string(rune('a'+i)), &run_v1alpha.Run{
			Task: "reindex", Trigger: run_v1alpha.MANUAL, Status: run_v1alpha.SUCCEEDED,
			EndedAt: now.Add(-time.Duration(i) * time.Minute),
		}))
	}
	old := h.put("run/old", &run_v1alpha.Run{
		Task: "reindex", Trigger: run_v1alpha.MANUAL, Status: run_v1alpha.SUCCEEDED,
		EndedAt: now.Add(-48 * time.Hour),
	})

	_, err := h.gc.RunGC(ctx, now)
	require.NoError(t, err)

	for _, id := range recent {
		assert.True(t, h.exists(id), "%s is over the count but inside the age floor", id)
	}
	assert.False(t, h.exists(old), "over the count and past the age floor")
}

// A sandbox whose run is gone has nothing that will ever collect it. Catching
// that is what makes the old exec-sandbox leak un-leakable rather than merely
// less likely.
func TestGCReapsSandboxesWhoseRunIsGone(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)

	sbID := entity.Id("sandbox/run-orphan-a1")
	var sb compute.Sandbox
	sb.Status = compute.RUNNING
	sb.Spec.LogAttribute = labelSet("miren.stage", "run", "miren.run", "run/vanished")

	_, err := h.inm.EAC.Put(ctx, wrap(entity.New(entity.DBId, sbID, sb.Encode), sbID))
	require.NoError(t, err)

	result, err := h.gc.RunGC(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, result.ReapedOrphans)

	resp, err := h.inm.EAC.Get(ctx, sbID.String())
	require.NoError(t, err)
	var got compute.Sandbox
	got.Decode(resp.Entity().Entity())
	assert.Equal(t, compute.STOPPED, got.Status)
}

// A sandbox that isn't a run's is none of this controller's business.
func TestGCLeavesNonRunSandboxesAlone(t *testing.T) {
	ctx := context.Background()
	h := newGCHarness(t)

	sbID := entity.Id("sandbox/service-web")
	var sb compute.Sandbox
	sb.Status = compute.RUNNING
	sb.Spec.LogAttribute = labelSet("miren.stage", "app-run", "miren.service", "web")

	_, err := h.inm.EAC.Put(ctx, wrap(entity.New(entity.DBId, sbID, sb.Encode), sbID))
	require.NoError(t, err)

	result, err := h.gc.RunGC(ctx, time.Now())
	require.NoError(t, err)
	assert.Zero(t, result.ReapedOrphans)

	resp, err := h.inm.EAC.Get(ctx, sbID.String())
	require.NoError(t, err)
	var got compute.Sandbox
	got.Decode(resp.Entity().Entity())
	assert.Equal(t, compute.RUNNING, got.Status)
}

// labelSet is a thin alias so the tests read the way the spec does.
func labelSet(kv ...string) types.Labels { return types.LabelSet(kv...) }
