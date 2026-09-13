package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/usage/usage_v1alpha"
)

// The metric labels are written with dots and stored with underscores. Querying
// the dotted form matches nothing and reports no error, so every generated query
// is checked for the stored spelling.
func TestQueriesUseStoredLabelSpelling(t *testing.T) {
	r := require.New(t)

	appKind := groupKey(labelApp, labelKind)

	queries := []string{
		cpuCoresQuery(labelSandbox, "", time.Minute, aggregateAvg),
		cpuCoresQuery(labelNode, "", time.Hour, aggregateMax),
		cpuCoresQuery(appKind, "", time.Hour, aggregateAvg),
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateAvg),
		memoryBytesQuery(appKind, "", time.Hour, aggregateLast),
		cpuCoresQuery(groupKey(labelSandbox, labelService, labelVersion, labelNode, labelKind), "", time.Hour, aggregateAvg),
		nodeGaugeQuery(metricNodeCPUCoresTotal, "", time.Minute, aggregateLast),
		sandboxCountQuery("", time.Hour),
		firstSeenQuery("", time.Hour),
		lastSeenQuery("", time.Hour),
	}

	for _, q := range queries {
		r.NotContains(q, "miren.", "a dotted label silently matches nothing: %s", q)
	}
}

// entity is the app id for deployed services but the sandbox id for addons,
// which makes it useless as a row key. Grouping by it would silently merge every
// sandbox of an app into one row.
func TestQueriesNeverGroupByEntity(t *testing.T) {
	r := require.New(t)

	queries := []string{
		cpuCoresQuery(labelSandbox, "", time.Minute, aggregateAvg),
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateMax),
	}

	for _, q := range queries {
		r.NotContains(q, "by (entity)", "entity is not a usable grouping key: %s", q)
	}
}

func TestCPUCoresQueryAggregates(t *testing.T) {
	r := require.New(t)

	// Consumption across the whole window already is the window's average, so
	// avg needs no subquery wrapper.
	avg := cpuCoresQuery(labelSandbox, "", 5*time.Minute, aggregateAvg)
	r.Equal("(sum by (miren_sandbox) (increase(cpu_usage_seconds_total[300s])) / 300)", avg)

	// max does need one, or a spike that has already passed averages away into
	// nothing -- which is the entire reason to ask for max.
	max := cpuCoresQuery(labelSandbox, "", time.Hour, aggregateMax)
	r.True(strings.HasPrefix(max, "max_over_time("), "max must collapse a subquery: %s", max)
	r.Contains(max, "[3600s:")
}

func TestCPUCoresQueryFloorsTheRateWindow(t *testing.T) {
	// A one-second range holds at most one sample of a one-second counter, and
	// an increase needs two. Asking for it returns nothing rather than
	// erroring, so the floor is what keeps a short window from reading as an
	// idle cluster.
	q := cpuCoresQuery(labelSandbox, "", time.Second, aggregateAvg)
	require.Contains(t, q, "[15s]")

	// The divisor has to be the range that was actually written, not the one
	// that was asked for, or the floor would scale the answer by fifteen.
	require.Contains(t, q, "/ 15")
}

// rate(m[d]) measures a series over its own samples rather than over d, so
// summing two sandboxes double-counts one that died inside the window: an app
// that redeployed reads as two apps. Measured against a steady one-core app,
// sum(rate()) answered 2.0 cores half an hour after a redeploy.
func TestCPUCoresQueryIsTimeWeightedAcrossSandboxes(t *testing.T) {
	r := require.New(t)

	for _, agg := range []string{aggregateAvg, aggregateMax, aggregateMin, aggregateLast} {
		q := cpuCoresQuery(groupKey(labelApp, labelKind), "", time.Hour, agg)
		r.NotContains(q, "rate(", "rate() is not time-weighted once summed: %s", q)
		r.Contains(q, "increase(", "%s", q)
	}
}

func TestMemoryQueryCollapsesGauge(t *testing.T) {
	r := require.New(t)

	// Memory is a gauge, so unlike CPU every aggregate needs an explicit
	// over_time; the bare selector would return one point, not a window.
	r.Equal("avg_over_time(sum by (miren_sandbox) (last_over_time(memory_usage_bytes[60s]))[60s:3s])",
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateAvg))
	r.Equal("max_over_time(sum by (miren_sandbox) (last_over_time(memory_usage_bytes[60s]))[60s:3s])",
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateMax))
	r.Equal("sum by (miren_sandbox) (last_over_time(memory_usage_bytes[60s]))",
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateLast))
}

// A bare selector inside a subquery inherits the subquery's step as its
// lookbehind, and that step grows with the window: over an hour at a day, over
// eight at a week. A sandbox that died keeps being summed alongside the one
// that replaced it for that long, so a steady 100 MiB app read as 200 MiB.
func TestMemoryQueryPinsItsLookbehind(t *testing.T) {
	r := require.New(t)

	for _, w := range []time.Duration{time.Minute, 24 * time.Hour, 168 * time.Hour} {
		q := memoryBytesQuery(groupKey(labelApp, labelKind), "", w, aggregateMax)
		r.Contains(q, "last_over_time(memory_usage_bytes[60s])",
			"the lookbehind must not follow the window: %s", q)
	}
}

// An app rollup groups by two labels at once, which is the only reason the
// grouping is built rather than named directly.
func TestGroupKeyJoinsLabels(t *testing.T) {
	r := require.New(t)

	r.Equal("miren_app", groupKey(labelApp))
	r.Equal("miren_app, miren_kind", groupKey(labelApp, labelKind))

	q := cpuCoresQuery(groupKey(labelApp, labelKind), "", time.Hour, aggregateAvg)
	r.Contains(q, "sum by (miren_app, miren_kind)")
}

// A sandbox writes one memory series per container, so counting series would
// report a sandbox with a sidecar as two. The nesting is what makes this a
// count of sandboxes.
func TestSandboxCountQueryCountsSandboxesNotSeries(t *testing.T) {
	r := require.New(t)

	q := sandboxCountQuery(labelSelector(map[string]string{labelApp: "shop"}), time.Hour)

	r.Equal(
		`count by (miren_app, miren_kind) (count by (miren_app, miren_kind, miren_sandbox) `+
			`(last_over_time(memory_usage_bytes{miren_app="shop"}[3600s])))`,
		q)
}

// tmin_over_time and tmax_over_time name the timestamp of the smallest and
// largest value, not the first and last sample. Using them here would report a
// sandbox as having started whenever its memory happened to dip, so the
// distinction is worth pinning down.
func TestSeenQueriesAskForSampleTimesNotValueExtremes(t *testing.T) {
	r := require.New(t)

	sel := labelSelector(map[string]string{labelApp: "shop"})

	r.Equal(
		`min by (miren_sandbox) (tfirst_over_time(memory_usage_bytes{miren_app="shop"}[3600s]))`,
		firstSeenQuery(sel, time.Hour))

	r.Equal(
		`max by (miren_sandbox) (tlast_over_time(memory_usage_bytes{miren_app="shop"}[3600s]))`,
		lastSeenQuery(sel, time.Hour))

	for _, q := range []string{firstSeenQuery(sel, time.Hour), lastSeenQuery(sel, time.Hour)} {
		r.NotContains(q, "tmin_over_time")
		r.NotContains(q, "tmax_over_time")
	}
}

func TestLabelSelector(t *testing.T) {
	r := require.New(t)

	r.Equal("", labelSelector(nil))
	r.Equal("", labelSelector(map[string]string{labelNode: ""}),
		"an empty filter value means unconstrained, not match-the-empty-string")

	r.Equal(`{miren_node="node/a"}`, labelSelector(map[string]string{labelNode: "node/a"}))

	// Ordering is fixed so the same filters always produce the same query
	// string, which is what makes these tests and any response cache work.
	r.Equal(`{miren_node="node/a",miren_sandbox="sb_1"}`,
		labelSelector(map[string]string{labelSandbox: "sb_1", labelNode: "node/a"}))
}

func TestLabelSelectorEscapesValues(t *testing.T) {
	// Filter values arrive from a URL query string. A value carrying a quote
	// must not be able to close the selector and append clauses of its own.
	q := labelSelector(map[string]string{labelNode: `a" or foo!="`})
	require.Equal(t, `{miren_node="a\" or foo!=\""}`, q)
}

func TestResolveAggregate(t *testing.T) {
	r := require.New(t)

	r.Equal(aggregateMax, resolveAggregate("MAX"))
	r.Equal(aggregateMin, resolveAggregate(" min "))
	r.Equal(aggregateLast, resolveAggregate("last"))

	// A typo answers with the default rather than failing. This is a call
	// someone makes when something is already wrong; refusing it over a
	// misspelled flag helps no one.
	r.Equal(aggregateAvg, resolveAggregate("maximum"))
	r.Equal(aggregateAvg, resolveAggregate(""))
}

// A range applied to an expression rather than a bare metric is a subquery, and
// a subquery with no resolution step returns an empty result and no error. That
// failure is invisible: it reads downstream as a sandbox consuming nothing.
func TestCollapsedQueriesAlwaysCarryASubqueryStep(t *testing.T) {
	r := require.New(t)

	collapsed := []string{
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateAvg),
		memoryBytesQuery(labelNode, "", time.Hour, aggregateMax),
		nodeGaugeQuery(metricNodeCPUCoresUsed, "", time.Minute, aggregateAvg),
		cpuCoresQuery(labelSandbox, "", time.Hour, aggregateMax),
	}

	for _, q := range collapsed {
		r.Contains(q, "_over_time(", "expected a collapsed query: %s", q)
		r.Regexp(`\[\d+s:\d+s\]`, q, "subquery is missing its resolution step: %s", q)
	}
}

// A max is asked for precisely to find a burst an average would hide. Measuring
// consumption across the whole window would flatten that burst into the
// window's mean, making max and avg identical and the flag useless.
func TestMaxUsesAShortRateWindowSteppedAcrossTheWindow(t *testing.T) {
	r := require.New(t)

	q := cpuCoresQuery(labelSandbox, "", time.Hour, aggregateMax)

	r.Contains(q, "increase(cpu_usage_seconds_total[180s])",
		"the range must cover a slice of the window, not all of it: %s", q)
	r.Contains(q, "[3600s:180s]", "the subquery must step across the window: %s", q)

	// The average is the one case where measuring across the whole window is
	// right, and it needs no subquery at all.
	avg := cpuCoresQuery(labelSandbox, "", time.Hour, aggregateAvg)
	r.Equal("(sum by (miren_sandbox) (increase(cpu_usage_seconds_total[3600s])) / 3600)", avg)
}

// An explicit order always wins; otherwise the comparator's own default
// applies. The direction no longer comes from guessing at the key name, because
// a key one listing sorts by name and another does not implement would get an
// alphabetical direction applied to a CPU comparison.
func TestOrderingDirection(t *testing.T) {
	r := require.New(t)

	r.True(ordering{}.direction(false), "a usage column defaults to busiest first")
	r.False(ordering{}.direction(true), "a name defaults to A to Z")

	r.False(ordering{order: "asc"}.direction(false))
	r.True(ordering{order: "desc"}.direction(true))
	r.False(ordering{order: "ASC"}.direction(false), "case-insensitive")
	r.True(ordering{order: "nonsense"}.direction(false), "an unparseable order falls back to the default")
}

// Every sort key a listing accepts must have a comparator. A key that reaches
// the default branch silently sorts by CPU, and if the key was name-like it
// also inherits an ascending direction, so the caller gets ascending CPU order
// with no indication anything was ignored.
//
// Each row's four name-like fields disagree on purpose, and each key has a
// different expected winner. A fixture that gave every field the same value
// would pass even if the app comparator read the service, because the two would
// sort identically.
func TestEverySortKeyHasAComparator(t *testing.T) {
	sandbox := func(short, app, service, node string, cores float64) *usage_v1alpha.SandboxUsage {
		var ref usage_v1alpha.SandboxRef
		ref.SetSandboxShortId(short)
		ref.SetApp(app)
		ref.SetService(service)
		ref.SetNodeName(node)

		var cpu usage_v1alpha.CpuUsage
		cpu.SetCores(cores)

		var row usage_v1alpha.SandboxUsage
		row.SetRef(&ref)
		row.SetCpu(&cpu)
		row.SetMemory(&usage_v1alpha.MemoryUsage{})
		return &row
	}

	// Rows are identified by short id. "filler" holds the lowest CPU and sorts
	// last on every name-like field, so it is what an ascending CPU fallthrough
	// would surface and it can never be the right answer for a name key.
	rows := func() []*usage_v1alpha.SandboxUsage {
		return []*usage_v1alpha.SandboxUsage{
			sandbox("d", "a", "c", "b", 4),
			sandbox("a", "d", "b", "c", 3),
			sandbox("c", "b", "a", "d", 2),
			sandbox("b", "c", "d", "a", 1),
			sandbox("filler", "filler", "filler", "filler", 0),
		}
	}

	// Each key wins on a different row, so a comparator reading the wrong
	// name-like field is caught rather than passing by coincidence.
	for key, wantFirst := range map[string]string{
		"name":    "a",
		"app":     "d",
		"service": "c",
		"node":    "b",
		"runner":  "b",
	} {
		got := rows()
		sortSandboxes(got, ordering{sort: key})

		assert.Equalf(t, wantFirst, got[0].Ref().SandboxShortId(),
			"sort=%s picked the wrong row; it may be reading another field, or falling through to CPU", key)
	}

	// A usage key still reads busiest first.
	got := rows()
	sortSandboxes(got, ordering{sort: "cpu"})
	assert.Equal(t, "d", got[0].Ref().SandboxShortId(), "cpu sorts busiest first")
}
