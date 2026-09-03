package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The metric labels are written with dots and stored with underscores. Querying
// the dotted form matches nothing and reports no error, so every generated query
// is checked for the stored spelling.
func TestQueriesUseStoredLabelSpelling(t *testing.T) {
	r := require.New(t)

	queries := []string{
		cpuCoresQuery(labelSandbox, "", time.Minute, aggregateAvg),
		cpuCoresQuery(labelNode, "", time.Hour, aggregateMax),
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateAvg),
		nodeGaugeQuery(metricNodeCPUCoresTotal, "", time.Minute, aggregateLast),
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

	// A rate over the whole window already is the window's average, so avg
	// needs no subquery wrapper.
	avg := cpuCoresQuery(labelSandbox, "", 5*time.Minute, aggregateAvg)
	r.Equal("sum by (miren_sandbox) (rate(cpu_usage_seconds_total[300s]))", avg)

	// max does need one, or a spike that has already passed averages away into
	// nothing -- which is the entire reason to ask for max.
	max := cpuCoresQuery(labelSandbox, "", time.Hour, aggregateMax)
	r.True(strings.HasPrefix(max, "max_over_time("), "max must collapse a subquery: %s", max)
	r.Contains(max, "[3600s:")
}

func TestCPUCoresQueryFloorsTheRateWindow(t *testing.T) {
	// A one-second range holds at most one sample of a one-second counter, and
	// a rate needs two. Asking for it returns nothing rather than erroring, so
	// the floor is what keeps a short window from reading as an idle cluster.
	q := cpuCoresQuery(labelSandbox, "", time.Second, aggregateAvg)
	require.Contains(t, q, "[15s]")
}

func TestMemoryQueryCollapsesGauge(t *testing.T) {
	r := require.New(t)

	// Memory is a gauge, so unlike CPU every aggregate needs an explicit
	// over_time; the bare selector would return one point, not a window.
	r.Equal("avg_over_time(sum by (miren_sandbox) (memory_usage_bytes)[60s:3s])",
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateAvg))
	r.Equal("max_over_time(sum by (miren_sandbox) (memory_usage_bytes)[60s:3s])",
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateMax))
	r.Equal("sum by (miren_sandbox) (memory_usage_bytes)",
		memoryBytesQuery(labelSandbox, "", time.Minute, aggregateLast))
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

// A max is asked for precisely to find a burst an average would hide. Taking
// the rate over the whole window would flatten that burst into the window's
// mean, making max and avg identical and the flag useless.
func TestMaxUsesAShortRateWindowSteppedAcrossTheWindow(t *testing.T) {
	r := require.New(t)

	q := cpuCoresQuery(labelSandbox, "", time.Hour, aggregateMax)

	r.Contains(q, "rate(cpu_usage_seconds_total[180s])",
		"the rate must cover a slice of the window, not all of it: %s", q)
	r.Contains(q, "[3600s:180s]", "the subquery must step across the window: %s", q)

	// The average is the one case where a full-window rate is right, and it
	// needs no subquery at all.
	avg := cpuCoresQuery(labelSandbox, "", time.Hour, aggregateAvg)
	r.Equal("sum by (miren_sandbox) (rate(cpu_usage_seconds_total[3600s]))", avg)
}

// The sensible default direction depends on what is being sorted: a usage
// column wants the busiest first, a name wants A to Z. An earlier version
// inverted the name comparator to fake that, which meant an explicit order=asc
// produced reverse-alphabetical output.
func TestOrderingDefaultsPerSortKey(t *testing.T) {
	r := require.New(t)

	r.True(ordering{sort: "cpu"}.descending(), "busiest first when unspecified")
	r.True(ordering{sort: ""}.descending(), "cpu is the default key, so descending")
	r.False(ordering{sort: "name"}.descending(), "names read A to Z")
	r.False(ordering{sort: "app"}.descending())
	r.False(ordering{sort: "service"}.descending())

	// An explicit direction always wins, in both directions and for both
	// families of key.
	r.False(ordering{sort: "cpu", order: "asc"}.descending())
	r.True(ordering{sort: "name", order: "desc"}.descending())
	r.False(ordering{sort: "name", order: "ASC"}.descending(), "case-insensitive")
}
