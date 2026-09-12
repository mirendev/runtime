package usage

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/metrics"
)

// row builds one directory entry. The ref is what the entity pass would have
// produced. It no longer carries usage: the figures come from the store now,
// and the entity pass says only which apps exist.
func row(sandbox, app, kind string) sandboxRow {
	var ref usage_v1alpha.SandboxRef
	ref.SetSandbox(sandbox)
	ref.SetApp(app)
	ref.SetAppId("app/" + app)
	ref.SetKind(kind)
	return sandboxRow{ref: &ref}
}

// sample is one group as the store returns it: an app, a kind, and what that
// slice of the app measured.
type sample struct {
	app   string
	kind  string
	cores float64
	bytes float64
	count int64
}

func storeSaw(samples ...sample) appMetrics {
	m := newAppMetrics()
	for _, s := range samples {
		k := appKey{app: s.app, kind: s.kind}
		m.cpu[k] = s.cores
		m.memory[k] = s.bytes
		m.counts[k] = s.count
	}
	return m
}

func findApp(rows []*usage_v1alpha.AppUsage, name string) *usage_v1alpha.AppUsage {
	for _, r := range rows {
		if r.App() == name {
			return r
		}
	}
	return nil
}

// The whole point of the rollup: an app's own services and the database it
// depends on are separate sandboxes, and a caller should not have to know that.
func TestAppRowsSumServicesAndAddons(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/web1", "shop", string(compute.KindApp)),
		row("sb/web2", "shop", string(compute.KindApp)),
		row("sb/pg", "shop", string(compute.KindAddon)),
	}}

	m := storeSaw(
		sample{app: "shop", kind: string(compute.KindApp), cores: 0.75, bytes: 200, count: 2},
		sample{app: "shop", kind: string(compute.KindAddon), cores: 1.5, bytes: 800, count: 1},
	)

	rows, cluster := buildAppRows(dir, m, "", true)
	require.Len(t, rows, 1)

	shop := rows[0]
	assert.InDelta(t, 2.25, shop.Total().CpuCores(), 0.001)
	assert.InDelta(t, 0.75, shop.Services().CpuCores(), 0.001)
	assert.InDelta(t, 1.5, shop.Addons().CpuCores(), 0.001)
	assert.Equal(t, int64(1000), shop.Total().MemoryBytes())

	// The counts say what the split is made of, so "1.5 cores of addon" can be
	// attributed to something.
	assert.Equal(t, int64(3), shop.SandboxCount())
	assert.Equal(t, int64(2), shop.ServiceCount())
	assert.Equal(t, int64(1), shop.AddonCount())

	assert.InDelta(t, 2.25, cluster.cpuCores, 0.001)
}

// Excluding addons answers "what is my code doing" rather than "what does this
// app cost". The addon block must then read zero rather than disappear, or the
// total and the split stop agreeing.
func TestAppRowsCanExcludeAddons(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/web", "shop", string(compute.KindApp)),
		row("sb/pg", "shop", string(compute.KindAddon)),
	}}

	m := storeSaw(
		sample{app: "shop", kind: string(compute.KindApp), cores: 0.5, count: 1},
		sample{app: "shop", kind: string(compute.KindAddon), cores: 1.5, count: 1},
	)

	rows, _ := buildAppRows(dir, m, "", false)
	require.Len(t, rows, 1)

	assert.InDelta(t, 0.5, rows[0].Total().CpuCores(), 0.001)
	assert.InDelta(t, 0, rows[0].Addons().CpuCores(), 0.001)
	assert.Equal(t, int64(0), rows[0].AddonCount())
	assert.Equal(t, int64(1), rows[0].SandboxCount())
}

func TestAppRowsSeparateApps(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/a", "shop", string(compute.KindApp)),
		row("sb/b", "blog", string(compute.KindApp)),
		row("sb/c", "blog", string(compute.KindAddon)),
	}}

	m := storeSaw(
		sample{app: "shop", kind: string(compute.KindApp), cores: 1, count: 1},
		sample{app: "blog", kind: string(compute.KindApp), cores: 2, count: 1},
		sample{app: "blog", kind: string(compute.KindAddon), cores: 3, count: 1},
	)

	rows, _ := buildAppRows(dir, m, "", true)
	require.Len(t, rows, 2)

	assert.InDelta(t, 1.0, findApp(rows, "shop").Total().CpuCores(), 0.001)
	assert.InDelta(t, 5.0, findApp(rows, "blog").Total().CpuCores(), 0.001)
}

// An app whose sandboxes have all gone quiet is a finding. Dropping the row
// would make a broken telemetry pipeline look like an idle cluster, which is
// the whole reason the entity pass still contributes rows.
func TestAppRowsReportSilentAppsAsStale(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/quiet", "shop", string(compute.KindApp)),
		row("sb/busy", "blog", string(compute.KindApp)),
	}}

	m := storeSaw(sample{app: "blog", kind: string(compute.KindApp), cores: 1, count: 1})

	rows, _ := buildAppRows(dir, m, "", true)
	require.Len(t, rows, 2, "an app with no samples still gets a row")

	quiet := findApp(rows, "shop")
	assert.True(t, quiet.Stale())
	assert.False(t, quiet.Historical(), "it still exists; it is just not reporting")

	assert.False(t, findApp(rows, "blog").Stale())
}

// The reason for sourcing rows from the store: a sandbox is swept from the
// entity store about an hour after it dies, and its samples are kept for a
// month. A window reaching past that hour can only be answered by the store.
func TestAppRowsIncludeAppsOnlyTheStoreRemembers(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/live", "blog", string(compute.KindApp)),
	}}

	m := storeSaw(
		sample{app: "blog", kind: string(compute.KindApp), cores: 1, count: 1},
		sample{app: "shop", kind: string(compute.KindApp), cores: 4, bytes: 500, count: 2},
	)

	rows, cluster := buildAppRows(dir, m, "", true)
	require.Len(t, rows, 2)

	gone := findApp(rows, "shop")
	require.NotNil(t, gone, "an app the entity store has forgotten still has usage to report")
	assert.True(t, gone.Historical())
	assert.False(t, gone.Stale(), "it reported; it just no longer exists")
	assert.InDelta(t, 4.0, gone.Total().CpuCores(), 0.001)
	assert.Equal(t, "", gone.AppId(), "there is no entity left to take an id from")

	assert.False(t, findApp(rows, "blog").Historical())
	assert.InDelta(t, 5.0, cluster.cpuCores, 0.001)
}

// Live apps keep the order the entity pass gave them; only the historical ones
// are appended, and in a fixed order, so two identical calls agree.
func TestAppRowsPlaceLiveAppsBeforeHistoricalOnes(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/live", "zebra", string(compute.KindApp)),
	}}

	m := storeSaw(
		sample{app: "zebra", kind: string(compute.KindApp), cores: 1, count: 1},
		sample{app: "beta", kind: string(compute.KindApp), cores: 1, count: 1},
		sample{app: "alpha", kind: string(compute.KindApp), cores: 1, count: 1},
	)

	rows, _ := buildAppRows(dir, m, "", true)

	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.App())
	}
	assert.Equal(t, []string{"zebra", "alpha", "beta"}, names)
}

// The counts now mean "sandboxes that reported", which is what the store can
// actually say. They no longer come from the entity pass at all.
func TestAppRowsCountReportingSandboxesNotEntities(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/web1", "shop", string(compute.KindApp)),
		row("sb/web2", "shop", string(compute.KindApp)),
	}}

	// Only one of the two ever reported.
	m := storeSaw(sample{app: "shop", kind: string(compute.KindApp), cores: 1, count: 1})

	rows, _ := buildAppRows(dir, m, "", true)
	require.Len(t, rows, 1)

	assert.Equal(t, int64(1), rows[0].SandboxCount())
	assert.Equal(t, int64(1), rows[0].ServiceCount())
}

// A sandbox belonging to no app -- a shared addon server -- has nothing to roll
// up into, and must not create a row named "".
func TestAppRowsSkipSandboxesWithNoApp(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/shared", "", string(compute.KindAddon)),
		row("sb/web", "shop", string(compute.KindApp)),
	}}

	m := storeSaw(sample{app: "shop", kind: string(compute.KindApp), cores: 1, count: 1})

	rows, cluster := buildAppRows(dir, m, "", true)

	require.Len(t, rows, 1)
	assert.Equal(t, "shop", rows[0].App())
	assert.InDelta(t, 1.0, cluster.cpuCores, 0.001,
		"an unattributable sandbox must not inflate the cluster total")
}

// The same rule on the metrics side, where it is enforced: a series with no app
// label never becomes a group.
func TestAppKeyOfRejectsUnattributableSeries(t *testing.T) {
	_, ok := appKeyOf(map[string]string{labelSandbox: "sb/shared"})
	assert.False(t, ok, "a series with no app label has nothing to roll up into")

	k, ok := appKeyOf(map[string]string{labelApp: "shop", labelKind: "addon"})
	require.True(t, ok)
	assert.Equal(t, appKey{app: "shop", kind: "addon"}, k)
}

func TestAppRowsFilterByApp(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/a", "shop", string(compute.KindApp)),
		row("sb/b", "blog", string(compute.KindApp)),
	}}

	m := storeSaw(
		sample{app: "shop", kind: string(compute.KindApp), cores: 1, count: 1},
		sample{app: "blog", kind: string(compute.KindApp), cores: 2, count: 1},
	)

	rows, _ := buildAppRows(dir, m, "shop", true)

	require.Len(t, rows, 1)
	assert.Equal(t, "shop", rows[0].App())
}

// --- series ---

// rangeStub answers query_range with one series per kind, recording what was
// asked for.
type rangeStub struct {
	queries []string
	step    string
	series  []metrics.Result
}

func (s *rangeStub) reader(t *testing.T) *metrics.VictoriaMetricsReader {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		require.NoError(t, err)

		s.queries = append(s.queries, q.Get("query"))
		s.step = q.Get("step")

		body, err := json.Marshal(metrics.QueryResult{
			Status: "success",
			Data:   metrics.Data{ResultType: "matrix", Result: s.series},
		})
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return metrics.NewVictoriaMetricsReader(slog.New(slog.DiscardHandler), srv.URL, 5*time.Second)
}

func kindSeries(kind string, samples ...[]any) metrics.Result {
	return metrics.Result{
		Metric: map[string]string{labelApp: "shop", labelKind: kind},
		Values: samples,
	}
}

func at(ts int64, value string) []any { return []any{float64(ts), value} }

func seriesNamed(out []*usage_v1alpha.UsageSeries, metric string) *usage_v1alpha.UsageSeries {
	for _, s := range out {
		if s.Metric() == metric {
			return s
		}
	}
	return nil
}

// An app's services and the database behind it are separate series in the
// store. A chart wants one line, so they are added at each timestamp -- not
// concatenated, which is what flattening them would do.
func TestAppSeriesSumsKindsPerTimestamp(t *testing.T) {
	stub := &rangeStub{series: []metrics.Result{
		kindSeries(string(compute.KindApp), at(100, "1"), at(160, "2")),
		kindSeries(string(compute.KindAddon), at(100, "0.5"), at(160, "0.25")),
	}}

	s := &Server{Reader: stub.reader(t)}
	w := window{start: time.Unix(100, 0), end: time.Unix(160, 0), aggregate: aggregateAvg}

	out, warnings := s.appSeries(context.Background(), "", w, time.Minute, true)
	require.Empty(t, warnings)
	require.Len(t, out, 2)

	cpu := seriesNamed(out, "cpu_cores")
	require.NotNil(t, cpu)

	points := cpu.Points()
	require.Len(t, points, 2, "two kinds at two timestamps is two points, not four")

	assert.InDelta(t, 1.5, points[0].Value(), 0.001)
	assert.InDelta(t, 2.25, points[1].Value(), 0.001)

	// Oldest first. A chart handed points out of order draws a scribble.
	assert.Equal(t, int64(100), points[0].At().Seconds())
	assert.Equal(t, int64(160), points[1].At().Seconds())
}

func TestAppSeriesDropsAddonsWhenExcluded(t *testing.T) {
	stub := &rangeStub{series: []metrics.Result{
		kindSeries(string(compute.KindApp), at(100, "1")),
		kindSeries(string(compute.KindAddon), at(100, "9")),
	}}

	s := &Server{Reader: stub.reader(t)}
	w := window{start: time.Unix(100, 0), end: time.Unix(160, 0), aggregate: aggregateAvg}

	out, _ := s.appSeries(context.Background(), "", w, time.Minute, false)

	cpu := seriesNamed(out, "cpu_cores")
	require.NotNil(t, cpu)
	require.Len(t, cpu.Points(), 1)
	assert.InDelta(t, 1.0, cpu.Points()[0].Value(), 0.001,
		"the addon's 9 cores must not reach a services-only series")
}

// Grouping by kind is not optional: it is what lets the addon exclusion above
// happen after the query rather than in it.
func TestAppSeriesGroupsByAppAndKind(t *testing.T) {
	stub := &rangeStub{}

	s := &Server{Reader: stub.reader(t)}
	w := window{start: time.Unix(100, 0), end: time.Unix(160, 0), aggregate: aggregateAvg}

	_, _ = s.appSeries(context.Background(), "", w, 30*time.Second, true)

	require.Len(t, stub.queries, 2)
	for _, q := range stub.queries {
		assert.Contains(t, q, "by (miren_app, miren_kind)")
	}
	assert.Equal(t, "30s", stub.step)
}

// step_seconds is what tells a charting client the resolution it was given.
// It stays unset when no series was drawn, so a caller cannot read a resolution
// into points it never received.
func TestWindowReportsTheStepItsSeriesUsed(t *testing.T) {
	w := window{start: time.Unix(0, 0), end: time.Unix(600, 0), aggregate: aggregateAvg}

	assert.Equal(t, int64(0), w.encode().StepSeconds())

	w.step = resolveStep(w, 0)
	assert.Equal(t, int64(10), w.encode().StepSeconds(),
		"ten minutes at the default point count is a ten-second step")

	w.step = resolveStep(w, 90*time.Second)
	assert.Equal(t, int64(90), w.encode().StepSeconds(), "an explicit step wins")

	// No collector here samples faster than once a second.
	w.step = resolveStep(w, time.Millisecond)
	assert.Equal(t, int64(1), w.encode().StepSeconds())
}
