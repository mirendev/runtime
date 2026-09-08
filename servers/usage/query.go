package usage

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Metric and label names as they exist in VictoriaMetrics.
//
// The collectors write attributes with dots (miren.sandbox), but the writer's
// sanitizeLabelName rewrites every dot to an underscore before the point is
// stored. Querying the dotted form matches nothing and returns no error, so
// these constants exist to keep the two spellings from drifting apart.
const (
	metricCPUSeconds = "cpu_usage_seconds_total"
	metricMemoryUsed = "memory_usage_bytes"

	metricNodeCPUCoresTotal = "node_cpu_cores_total"
	metricNodeCPUCoresUsed  = "node_cpu_cores_used"
	metricNodeMemTotal      = "node_memory_total_bytes"
	metricNodeMemUsed       = "node_memory_used_bytes"
	metricNodeStorageTotal  = "node_storage_total_bytes"
	metricNodeStorageUsed   = "node_storage_used_bytes"
	metricNodeLoad1         = "node_load1"
	metricNodeLoad5         = "node_load5"
	metricNodeLoad15        = "node_load15"

	labelSandbox = "miren_sandbox"
	labelNode    = "miren_node"
	labelRunner  = "miren_runner"
)

// Aggregates a caller may ask for.
const (
	aggregateAvg  = "avg"
	aggregateMax  = "max"
	aggregateMin  = "min"
	aggregateLast = "last"
)

// defaultWindow is the lookback when a caller does not ask for one. A minute is
// long enough that a one-second sampler has ~60 points to average, and short
// enough that the answer still describes "now".
const defaultWindow = time.Minute

// minRateWindow is the shortest range a rate() is asked for. The collectors
// sample once a second, but a counter needs at least two points inside the
// range to produce a rate at all, and a range shorter than the scrape interval
// yields nothing rather than an error.
const minRateWindow = 15 * time.Second

// resolveAggregate maps a caller's aggregate to a known one, defaulting to avg.
// An unrecognized value is not an error: it is more useful to answer a typo'd
// aggregate with the sensible default than to fail a diagnostic call outright.
func resolveAggregate(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case aggregateMax:
		return aggregateMax
	case aggregateMin:
		return aggregateMin
	case aggregateLast:
		return aggregateLast
	default:
		return aggregateAvg
	}
}

// promDuration renders a duration the way MetricsQL wants it. Sub-second
// precision is dropped because no collector here samples that fast, and a "0s"
// range would match nothing.
func promDuration(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	return fmt.Sprintf("%ds", int64(d.Seconds()))
}

// cpuCoresQuery builds the CPU query for one grouping label.
//
// CPU is stored as a cumulative counter of seconds spent on CPU, so a rate over
// a range is already an average over that range in cores -- which is why avg
// needs no subquery and max does. The sum is what collapses a sandbox's
// several containers, and any stray per-process labels, into one series per
// group; without it a sandbox whose runner restarted mid-window would appear
// twice.
func cpuCoresQuery(groupBy string, selector string, window time.Duration, aggregate string) string {
	// The rate window differs by aggregate, and getting it wrong is silent.
	//
	// For an average, the rate is taken over the whole window, because a rate
	// across a span already is that span's mean. For a max or a min the rate
	// must instead be taken over a short slice and stepped across the window:
	// a rate over the whole hour would flatten a one-minute spike into the
	// hour's average, so a "max" computed that way would equal the average and
	// never surface the burst it was asked to find.
	rateWindow := window
	if aggregate == aggregateMax || aggregate == aggregateMin {
		rateWindow = window / subqueryPoints
	}
	if rateWindow < minRateWindow {
		rateWindow = minRateWindow
	}

	inner := fmt.Sprintf("sum by (%s) (rate(%s%s[%s]))",
		groupBy, metricCPUSeconds, selector, promDuration(rateWindow))

	switch aggregate {
	case aggregateMax, aggregateMin:
		return fmt.Sprintf("%s_over_time(%s[%s:%s])",
			aggregate, inner, promDuration(window), promDuration(rateWindow))
	default:
		// avg and last both collapse to the plain rate: a rate over the whole
		// window IS its average, and there is no cheaper "last" for a counter.
		return inner
	}
}

// memoryBytesQuery builds the memory query for one grouping label.
//
// Memory is a gauge, so unlike CPU it needs an explicit over_time collapse for
// every aggregate except last.
func memoryBytesQuery(groupBy string, selector string, window time.Duration, aggregate string) string {
	inner := fmt.Sprintf("sum by (%s) (%s%s)", groupBy, metricMemoryUsed, selector)

	if aggregate == aggregateLast {
		return inner
	}

	return overTime(aggregate, inner, window)
}

// nodeGaugeQuery reads one node-level gauge, collapsed over the window.
//
// Capacity gauges use last rather than an average: a host's core count does not
// have a meaningful average, and averaging across a resize would report a
// machine size that never existed.
func nodeGaugeQuery(metric string, selector string, window time.Duration, aggregate string) string {
	inner := fmt.Sprintf("max by (%s) (%s%s)", labelNode, metric, selector)

	if aggregate == aggregateLast {
		return inner
	}

	return overTime(aggregate, inner, window)
}

// overTime wraps an expression in an <aggregate>_over_time subquery.
//
// The resolution after the colon is not optional. A range applied to an
// expression rather than to a bare metric is a subquery, and without a step the
// query returns an empty result and no error -- which reads downstream as a
// sandbox using nothing at all. Every collapse goes through here so that step
// can never be forgotten at one call site.
func overTime(aggregate, expr string, window time.Duration) string {
	step := window / subqueryPoints
	if step < time.Second {
		step = time.Second
	}

	return fmt.Sprintf("%s_over_time(%s[%s:%s])",
		aggregate, expr, promDuration(window), promDuration(step))
}

// subqueryPoints is how many samples a collapsed window is evaluated at. Enough
// that a max is not dominated by where the boundaries happen to fall, few enough
// that a day-long window does not evaluate thousands of points per row.
const subqueryPoints = 20

// labelSelector renders an exact-match selector, or "" when unconstrained.
// Values are escaped because a node name or app name reaches this from a query
// string and must not be able to close the selector and append its own clauses.
func labelSelector(pairs map[string]string) string {
	if len(pairs) == 0 {
		return ""
	}

	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		if pairs[k] != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}

	slices.Sort(keys)

	clauses := make([]string, 0, len(keys))
	for _, k := range keys {
		// %q is what does the escaping here. These values arrive from a query
		// string, so a name containing a quote must not be able to close the
		// selector and append clauses of its own.
		clauses = append(clauses, fmt.Sprintf("%s=%q", k, pairs[k]))
	}

	return "{" + strings.Join(clauses, ",") + "}"
}
