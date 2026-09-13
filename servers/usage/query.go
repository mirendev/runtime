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
	labelApp     = "miren_app"
	labelKind    = "miren_kind"
	labelService = "miren_service"
	labelVersion = "miren_version"
)

// groupKey renders several labels as one grouping clause.
//
// The query builders take the grouping as a single string because most callers
// group by one label. An app rollup needs two -- the app and the kind, so an
// app's own services stay separable from the addons it owns -- and this is what
// keeps the joining spelled in one place rather than at each call site.
func groupKey(labels ...string) string {
	return strings.Join(labels, ", ")
}

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

// promSeconds renders a duration the way MetricsQL wants it and reports the
// number of seconds it actually wrote. A caller dividing by the range has to
// divide by what went into the query rather than by what it asked for, or a
// floored sub-second range would scale the answer.
//
// Sub-second precision is dropped because no collector here samples that fast,
// and a "0s" range would match nothing.
func promSeconds(d time.Duration) (string, int64) {
	if d < time.Second {
		d = time.Second
	}
	secs := int64(d.Seconds())
	return fmt.Sprintf("%ds", secs), secs
}

func promDuration(d time.Duration) string {
	rendered, _ := promSeconds(d)
	return rendered
}

// sampleStaleness bounds how long one sandbox's last sample stays current.
//
// It exists because a bare selector inside a subquery inherits the subquery's
// step as its lookbehind, and that step grows with the window: at a day it is
// over an hour, at a week over eight. A sandbox that died keeps being counted
// for that long, so across a redeploy two generations are summed and an app
// reads as twice its size. Pinning the lookbehind bounds the overlap to about
// one scrape at any window length.
//
// Twelve times the writer's five-second flush period, so a runner that batches
// slowly still reports rather than leaving a hole in the series.
const sampleStaleness = time.Minute

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

	rangeStr, rangeSecs := promSeconds(rateWindow)

	// Consumption divided by the range, rather than rate(), because the two
	// disagree the moment more than one series is summed.
	//
	// rate(m[d]) is measured over the samples the series itself has, not over
	// d. One sandbox cannot double itself, so per sandbox the distinction never
	// showed. Summed across sandboxes it does: a sandbox that died inside the
	// window keeps contributing the rate it ran at while alive, for as long as
	// the window still holds its samples, so an app that redeployed reads as
	// two apps. Measured against a steady one-core app half an hour after a
	// redeploy, sum(rate()) answered 2.0 cores.
	//
	// increase() over the range, divided by the range, is time-weighted instead
	// -- what each sandbox actually consumed, spread across the window it was
	// asked about. VictoriaMetrics rewrites sum(rate()) into exactly this at
	// ranges of three hours or more, which is why long windows looked right and
	// short ones did not, and why a range query never looked right at all. It
	// is spelled out here so the answer does not depend on that rewrite.
	inner := fmt.Sprintf("(sum by (%s) (increase(%s%s[%s])) / %d)",
		groupBy, metricCPUSeconds, selector, rangeStr, rangeSecs)

	switch aggregate {
	case aggregateMax, aggregateMin:
		return fmt.Sprintf("%s_over_time(%s[%s:%s])",
			aggregate, inner, promDuration(window), rangeStr)
	default:
		// avg and last both collapse to the plain figure: consumption across
		// the whole window already is that window's average, and there is no
		// cheaper "last" for a counter.
		return inner
	}
}

// memoryBytesQuery builds the memory query for one grouping label.
//
// Memory is a gauge, so unlike CPU it needs an explicit over_time collapse for
// every aggregate except last.
func memoryBytesQuery(groupBy string, selector string, window time.Duration, aggregate string) string {
	// last_over_time rather than the bare selector: see sampleStaleness. Without
	// it the lookbehind follows the subquery step, and a dead sandbox is summed
	// alongside the one that replaced it for hours.
	inner := fmt.Sprintf("sum by (%s) (last_over_time(%s%s[%s]))",
		groupBy, metricMemoryUsed, selector, promDuration(sampleStaleness))

	if aggregate == aggregateLast {
		return inner
	}

	return overTime(aggregate, inner, window)
}

// sandboxCountQuery counts the sandboxes that reported, per group.
//
// This is how many sandboxes a row is made of, sourced from the store rather
// than from the entity pass so that a historical window can still say. It
// counts *reporting* sandboxes, which is not the same as scheduled ones: a
// sandbox that was never able to start never reports and so is never counted.
//
// The nesting is what makes it a sandbox count rather than a series count. A
// sandbox writes one memory series per container, so the inner count collapses
// each sandbox to one before the outer one counts them. Memory is the metric to
// count on because it is a gauge -- every live sandbox has a current value,
// where a CPU counter that has not advanced in the window yields no rate.
func sandboxCountQuery(selector string, window time.Duration) string {
	perSandbox := fmt.Sprintf("count by (%s) (last_over_time(%s%s[%s]))",
		groupKey(labelApp, labelKind, labelSandbox), metricMemoryUsed, selector, promDuration(window))

	return fmt.Sprintf("count by (%s) (%s)", groupKey(labelApp, labelKind), perSandbox)
}

// contributorMemoryQuery and contributorCPUSecondsQuery read what one sandbox
// used while it was running.
//
// Neither uses a subquery, and that is the whole point. Every other collapse
// here evaluates a fixed number of points across the window (see overTime), so
// the resolution degrades as the window grows: at a week the step is over eight
// hours, and a sandbox that lived ten minutes falls between two evaluation
// points and reports nothing at all. It still appears in the breakdown, with
// zeroes beside it, which reads as "used nothing" rather than "not measured".
//
// Applied straight to a raw metric these functions scan the samples themselves,
// so they are exact at any window length and describe the sandbox's own
// lifetime rather than the window's. A ten-minute sandbox reports the memory it
// actually held for those ten minutes.
//
// The sum is what collapses a sandbox's several containers into one figure.
func contributorMemoryQuery(groupBy, selector string, window time.Duration, aggregate string) string {
	return fmt.Sprintf("sum by (%s) (%s_over_time(%s%s[%s]))",
		groupBy, aggregate, metricMemoryUsed, selector, promDuration(window))
}

// contributorCPUSecondsQuery reads how much CPU time each sandbox consumed.
//
// Seconds rather than cores because seconds are what add up. Cores is a rate,
// and two sandboxes that ran for different lengths of time cannot be compared
// by it; the caller divides by the sandbox's own active span to recover a rate
// that means something.
func contributorCPUSecondsQuery(groupBy, selector string, window time.Duration) string {
	return fmt.Sprintf("sum by (%s) (increase(%s%s[%s]))",
		groupBy, metricCPUSeconds, selector, promDuration(window))
}

// firstSeenQuery and lastSeenQuery report when a sandbox's samples start and
// stop inside the window.
//
// Together they are the span a sandbox accounted for, which is what turns a
// week's worth of contributors from an undated pile into a sequence of
// deployments. Nothing else can supply it: the sandbox entity that knew its own
// start and exit times is swept about an hour after it dies.
//
// tfirst_over_time and tlast_over_time are the right functions and tmin/tmax
// are not, though the names suggest otherwise. tmin_over_time returns the
// timestamp of the smallest *value*, not the earliest sample, so a sandbox
// whose memory dipped at the end would report having started then.
//
// Memory is what they ask about because it is a gauge: every live sandbox has a
// current value, where a CPU counter that has not advanced yields no rate and
// would read as a sandbox that was never there.
func firstSeenQuery(selector string, window time.Duration) string {
	return fmt.Sprintf("min by (%s) (tfirst_over_time(%s%s[%s]))",
		labelSandbox, metricMemoryUsed, selector, promDuration(window))
}

func lastSeenQuery(selector string, window time.Duration) string {
	return fmt.Sprintf("max by (%s) (tlast_over_time(%s%s[%s]))",
		labelSandbox, metricMemoryUsed, selector, promDuration(window))
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
