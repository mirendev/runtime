package metrics

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newCappedMetrics() *HTTPMetrics {
	return &HTTPMetrics{
		counters:   map[string]*counterState{},
		candidates: map[string]uint64{},
	}
}

// newRecordingMetrics wires up enough to drive RecordRequest. An unstarted
// writer buffers points instead of sending them, which is what lets a test read
// back the labels that would have reached VictoriaMetrics.
func newRecordingMetrics(now func() time.Time) *HTTPMetrics {
	h := newCappedMetrics()
	h.Log = slog.New(slog.DiscardHandler)
	h.Writer = &VictoriaMetricsWriter{}
	h.instance = "test-instance"
	h.now = now
	return h
}

// hit is one request's worth of admission for a path. One entry per path now,
// so this is a single lookup, not the request/duration pair it used to be.
func hit(h *HTTPMetrics, path string) {
	h.trackCounter(counterKey("app", "GET", path))
}

func isTracked(h *HTTPMetrics, path string) bool {
	c, ok := h.counters[counterKey("app", "GET", path)]
	return ok && c.pathBound
}

func record(t *testing.T, h *HTTPMetrics, path string, status int, durationMs int64) {
	t.Helper()
	require.NoError(t, h.RecordRequest(context.Background(), HTTPRequest{
		App: "app", Method: "GET", Path: path, StatusCode: status, DurationMs: durationMs,
	}))
}

// TestCounterMapStaysBounded is the whole point of the cap. Path comes from the
// client, so a walk over unique URLs used to grow the map for as long as the
// process lived -- 780k keys and 83MB in the 0.14.0 heap dump.
func TestCounterMapStaysBounded(t *testing.T) {
	h := newCappedMetrics()

	for i := range 10_000 {
		hit(h, "/scan/"+strconv.Itoa(i))
	}

	require.Equal(t, maxPathBoundCounters, h.pathBound,
		"tracked counters must stop at the cap no matter how many paths arrive")
	require.LessOrEqual(t, len(h.counters), maxPathBoundCounters)
	require.LessOrEqual(t, len(h.candidates), maxCandidates)
}

// TestCounterMapStaysBoundedAcrossWindows: promotion runs only every
// promotionWindow touches, so check the map does not creep upward between
// passes. It cannot -- the cap is enforced on every admission, and the window
// only decides which keys hold the slots, never how many there are.
func TestCounterMapStaysBoundedAcrossWindows(t *testing.T) {
	h := newCappedMetrics()

	maxCounters, maxCands := 0, 0
	for i := range 500_000 {
		hit(h, "/scan/"+strconv.Itoa(i))
		maxCounters = max(maxCounters, len(h.counters))
		maxCands = max(maxCands, len(h.candidates))
	}

	require.Greater(t, h.touches, uint64(promotionWindow*10), "the run has to cross many windows")
	require.LessOrEqual(t, maxCounters, maxPathBoundCounters)
	require.LessOrEqual(t, maxCands, maxCandidates)
}

// TestFloodDoesNotEvictBusyPaths is the attack the eviction policy exists to
// survive. An earlier version closed its promotion window once the candidate
// table filled, which a client walking unique URLs controls completely: windows
// closed continuously, per-window decay drove every real path's score to zero,
// and a one-hit URL then outranked all of them. It took 25 of 25 busy paths.
//
// The flood is long enough to decay a score of 100 to zero -- seven halvings,
// so seven windows -- and then some. A shorter flood passes even with
// minPromotionHits removed, and so proves nothing about the floor.
func TestFloodDoesNotEvictBusyPaths(t *testing.T) {
	h := newCappedMetrics()

	for range 100 {
		for i := range maxPathBoundCounters {
			hit(h, "/real/"+strconv.Itoa(i))
		}
	}
	for i := range maxPathBoundCounters {
		require.True(t, isTracked(h, "/real/"+strconv.Itoa(i)), "busy paths should start tracked")
	}

	for i := range 100_000 {
		hit(h, "/junk/"+strconv.Itoa(i))
	}

	survivors := 0
	for i := range maxPathBoundCounters {
		if isTracked(h, "/real/"+strconv.Itoa(i)) {
			survivors++
		}
	}
	require.Equal(t, maxPathBoundCounters, survivors,
		"a flood of one-hit URLs must not displace paths carrying real traffic")
}

// TestOverflowEntriesAreExemptFromCap: the sink untracked traffic lands in must
// not compete for the slots it exists to protect.
func TestOverflowEntriesAreExemptFromCap(t *testing.T) {
	h := newCappedMetrics()

	for i := range 100 {
		hit(h, "/scan/"+strconv.Itoa(i))
	}
	require.Equal(t, maxPathBoundCounters, h.pathBound)

	h.overflowCounter(counterKey("app", overflowLabel, overflowLabel))
	h.overflowCounter(counterKey("other-app", overflowLabel, overflowLabel))

	require.Equal(t, maxPathBoundCounters, h.pathBound, "overflow entries must not count against the cap")
	require.Greater(t, len(h.counters), maxPathBoundCounters, "they do still live in the same map")
}

// TestBusyPathDisplacesQuietOne is the "re-evaluated" half. First-seen-wins
// would let a crawler squat every slot at startup and never give them back.
func TestBusyPathDisplacesQuietOne(t *testing.T) {
	h := newCappedMetrics()

	for i := range maxPathBoundCounters {
		hit(h, "/quiet/"+strconv.Itoa(i))
	}
	require.Equal(t, maxPathBoundCounters, h.pathBound)
	require.False(t, isTracked(h, "/busy"), "the cap is full, so /busy starts out untracked")

	for range 2 * promotionWindow {
		hit(h, "/busy")
	}

	require.True(t, isTracked(h, "/busy"), "a path this busy must earn a slot from a quiet one")
	require.Equal(t, maxPathBoundCounters, h.pathBound, "promotion swaps a slot, it does not add one")
}

// TestPromotedCounterStartsAtZero guards the reported values: a promoted path's
// candidacy traffic was already counted under the overflow entry, so carrying it
// over would report those requests twice. The incumbents are given real counts
// first, so an implementation that recycled an evicted entry's struct gets
// caught rather than passing on a field that happens to be zero anyway.
func TestPromotedCounterStartsAtZero(t *testing.T) {
	now := time.Now()
	h := newCappedMetrics()

	for i := range maxPathBoundCounters {
		path := "/quiet/" + strconv.Itoa(i)
		hit(h, path)
		h.counters[counterKey("app", "GET", path)].observe("200", 200, 5, now)
	}
	for i := range maxPathBoundCounters {
		require.NotEmpty(t, h.counters[counterKey("app", "GET", "/quiet/"+strconv.Itoa(i))].requests,
			"incumbents must carry real counts, or this test cannot fail")
	}

	for range 2 * promotionWindow {
		hit(h, "/busy")
	}

	c := h.counters[counterKey("app", "GET", "/busy")]
	require.NotNil(t, c)
	require.Empty(t, c.requests, "a promoted counter reports from zero, not from its candidacy")
	require.Zero(t, c.durationCount)
}

// TestOverflowLabelsReachExportedPoints pins the half of the fix that bounds
// VictoriaMetrics rather than the heap. Without it, a regression putting
// req.Path back into the label maps keeps every other test green while series
// cardinality goes unbounded again.
func TestOverflowLabelsReachExportedPoints(t *testing.T) {
	now := time.Now()
	h := newRecordingMetrics(func() time.Time { return now })

	last := "/scan/" + strconv.Itoa(maxPathBoundCounters+4)
	for i := range maxPathBoundCounters + 5 {
		record(t, h, "/scan/"+strconv.Itoa(i), 200, 3)
	}

	var sawOverflow bool
	for _, p := range h.Writer.buffer {
		require.NotEqual(t, last, p.Labels["path"], "a path past the cap must never reach an exported label")
		if p.Labels["path"] == overflowLabel {
			sawOverflow = true
			require.Equal(t, overflowLabel, p.Labels["method"], "method collapses along with the path")
		}
	}
	require.True(t, sawOverflow, "traffic past the cap still has to be exported, under the overflow label")
}

// TestTopPathsReportsErrorsForTrackedPath is the corruption this change exists
// to remove. Statuses used to be separate map entries admitted and evicted
// independently, so a path could keep its 200 counter while its 500 counter fell
// into the overflow bucket, and the per-path error rate read zero. Error
// statuses are rarer than successes, so they were the weakest entries and the
// first evicted: the paths that had errors were the ones that lost them.
func TestTopPathsReportsErrorsForTrackedPath(t *testing.T) {
	now := time.Now()
	h := newRecordingMetrics(func() time.Time { return now })

	for range 9 {
		record(t, h, "/api", 200, 10)
	}
	record(t, h, "/api", 500, 30)
	for range 4 {
		record(t, h, "/health", 200, 1)
	}

	paths, err := h.TopPaths("app", 5)
	require.NoError(t, err)
	require.Len(t, paths, 2)

	require.Equal(t, "/api", paths[0].Path, "busiest path first")
	require.EqualValues(t, 10, paths[0].Count)
	require.InDelta(t, 0.1, paths[0].ErrorRate, 0.0001, "one 500 in ten requests")
	require.InDelta(t, 12.0, paths[0].AvgDurationMs, 0.0001, "(9*10 + 30) / 10")

	require.Equal(t, "/health", paths[1].Path)
	require.Zero(t, paths[1].ErrorRate)
}

// TestTopPathsExcludesOverflow: "other" is not a path anyone can act on, and it
// would outrank every real one on a busy app.
func TestTopPathsExcludesOverflow(t *testing.T) {
	now := time.Now()
	h := newRecordingMetrics(func() time.Time { return now })

	record(t, h, "/real", 200, 1)
	for i := range 200 {
		for range 3 {
			record(t, h, "/scan/"+strconv.Itoa(i), 404, 1)
		}
	}

	paths, err := h.TopPaths("app", 10)
	require.NoError(t, err)
	for _, p := range paths {
		require.NotEqual(t, overflowLabel, p.Path, "the overflow bucket is not a path")
	}
}

// TestTopPathsWindowExpires: the numbers are a rolling hour, matching the [1h]
// range of the query this replaced. Cumulative-since-start would let a path that
// was busy yesterday outrank one busy now.
func TestTopPathsWindowExpires(t *testing.T) {
	now := time.Now()
	h := newRecordingMetrics(func() time.Time { return now })

	for range 5 {
		record(t, h, "/api", 200, 10)
	}

	paths, err := h.TopPaths("app", 5)
	require.NoError(t, err)
	require.Len(t, paths, 1)

	now = now.Add(2 * time.Hour)
	paths, err = h.TopPaths("app", 5)
	require.NoError(t, err)
	require.Empty(t, paths, "traffic older than the window must age out")
}

// TestLongPathIsTruncated: the cap counts entries, so an entry has to have a
// bounded size. The ingress allows a ~1MB request line, and the path went into
// both the key and the exported label verbatim.
func TestLongPathIsTruncated(t *testing.T) {
	now := time.Now()
	h := newRecordingMetrics(func() time.Time { return now })

	record(t, h, "/"+strings.Repeat("a", 10_000), 200, 1)

	for key, c := range h.counters {
		require.LessOrEqual(t, len(c.path), maxKeyPathBytes+3, "stored path must be truncated")
		require.LessOrEqual(t, len(key), maxKeyPathBytes+64, "the map key must be truncated too")
	}
	for _, p := range h.Writer.buffer {
		require.LessOrEqual(t, len(p.Labels["path"]), maxKeyPathBytes+3, "exported label must be truncated")
	}
}
