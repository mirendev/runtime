package metrics

import (
	"cmp"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Cardinality bounds for the counter map. Both path and method come from the
// client, so without a cap the map grows for as long as the process runs: a
// 0.14.0 heap dump had it holding ~780k keys and 83MB, 70% of the live heap.
const (
	// maxPathBoundCounters is how many counters carrying a real path and method
	// are kept. Everything else folds into the overflow entries.
	//
	// This is one budget shared by every app on the node, not a per-app
	// allowance, so it has to cover the busy routes of all of them at once.
	// Each entry is one path -- statuses and durations live inside it -- and
	// costs roughly 3KB, most of that the per-minute ring TopPaths reads, so
	// the whole table is a few hundred KB against the 83MB it replaces.
	maxPathBoundCounters = 100

	// maxCandidates bounds the side table that spots an untracked key busier
	// than one already tracked. Once full it stops accepting new keys until the
	// next window, but keeps counting the ones it already holds.
	maxCandidates = 100

	// promotionWindow is how many counter touches pass between promotion
	// passes. Deliberately a measure of traffic and not of distinct keys: tying
	// it to cardinality lets a client walking unique URLs close windows as fast
	// as it likes, and since every window decays the tracked hits, that drives
	// every real path to zero and hands all 25 slots to the scan.
	promotionWindow = 10_000

	// minPromotionHits is the floor a candidate has to clear before it can take
	// a slot, however quiet the slot's current occupant is. A scan sees each URL
	// once; real traffic repeats. Without this, once decay drives the weakest
	// tracked entry to zero, any one-hit URL outranks it.
	minPromotionHits = 8

	// overflowLabel stands in for the path and method of untracked traffic.
	// That traffic is still counted, just not per path. The labels left on an
	// overflow entry are app and status, neither of which the client chooses,
	// so these entries are bounded without needing a cap of their own.
	overflowLabel = "other"

	// maxKeyPathBytes truncates the path before it reaches a key or a label.
	// The cap above counts entries, but a key embeds the path verbatim and the
	// ingress leaves MaxHeaderBytes at Go's 1MB default, so 100 candidates
	// holding megabyte paths is ~100MB -- the same order as the leak this file
	// exists to bound. Counting entries only bounds memory if an entry has a
	// bounded size.
	maxKeyPathBytes = 256

	// recentMinutes is the span TopPaths reports over, kept as a ring of
	// per-minute buckets. It matches the [1h] the VictoriaMetrics query it
	// replaced used, so `miren app status` keeps meaning "busiest in the last
	// hour" rather than "busiest since this process started".
	recentMinutes = 60
)

// HTTPMetrics tracks HTTP request metrics for applications using VictoriaMetrics
type HTTPMetrics struct {
	Log    *slog.Logger
	Writer *VictoriaMetricsWriter
	Reader *VictoriaMetricsReader

	mu sync.Mutex
	// counters holds at most maxPathBoundCounters entries carrying a real path,
	// plus the overflow entries everything else accumulates into.
	counters  map[string]*counterState
	pathBound int
	// candidates counts hits for keys that missed the cap, so a path that turns
	// busy can take a slot from one that has gone quiet.
	candidates map[string]uint64
	// touches counts counter lookups, and closes a promotion window every
	// promotionWindow of them.
	touches  uint64
	instance string

	// now is time.Now in production; tests set it to drive the rolling window.
	now func() time.Time
}

// counterState is everything known about one app/method/path. Statuses and
// durations live together deliberately: when they were separate map entries
// they were admitted and evicted independently, so a path could keep its 200
// counter while its 500 counter fell into the overflow bucket. TopPaths then
// reported that path's error rate as zero -- and since error statuses are rarer
// than successes, they were the weakest entries and the first evicted, which
// meant the paths that had errors were exactly the ones whose errors vanished.
type counterState struct {
	// app, method and path are held rather than parsed back out of the map key,
	// so TopPaths can report without splitting strings.
	app    string
	method string
	path   string

	// requests is the cumulative count per status, one exported series each.
	requests      map[string]float64
	durationSum   float64
	durationCount float64

	// recent is a ring of per-minute buckets covering recentMinutes, which is
	// what TopPaths reads. The cumulative fields above stay monotonic for
	// VictoriaMetrics; these expire.
	recent [recentMinutes]minuteBucket

	// hits ranks entries for eviction. It counts every touch rather than any
	// one reported value, and is halved each promotion window so a path that
	// was busy once cannot hold a slot forever.
	hits uint64

	// pathBound marks the entries the cap applies to. Overflow entries are
	// exempt: they are the sink untracked traffic lands in.
	pathBound bool
}

// minuteBucket is one minute of the rolling window. minute records which
// absolute minute the bucket holds, so a stale bucket reads as empty instead of
// as an hour-old count, and gaps in traffic need no sweeping.
type minuteBucket struct {
	minute        int64
	count         int64
	errors        int64
	durationSum   float64
	durationCount int64
}

// observe folds one request into both the cumulative totals and the rolling
// window. Callers must hold h.mu.
func (c *counterState) observe(status string, statusCode int, durationMs int64, now time.Time) {
	if c.requests == nil {
		c.requests = make(map[string]float64, 4)
	}
	c.requests[status]++

	seconds := float64(durationMs) / 1000.0
	c.durationSum += seconds
	c.durationCount++

	minute := now.Unix() / 60
	b := &c.recent[minute%recentMinutes]
	if b.minute != minute {
		*b = minuteBucket{minute: minute}
	}
	b.count++
	if statusCode >= 400 {
		b.errors++
	}
	b.durationSum += seconds
	b.durationCount++
}

// window sums the buckets still inside recentMinutes of now.
func (c *counterState) window(now time.Time) minuteBucket {
	oldest := now.Unix()/60 - recentMinutes + 1
	var total minuteBucket
	for i := range c.recent {
		if b := c.recent[i]; b.minute >= oldest {
			total.count += b.count
			total.errors += b.errors
			total.durationSum += b.durationSum
			total.durationCount += b.durationCount
		}
	}
	return total
}

// NewHTTPMetrics creates a new HTTPMetrics with the given dependencies.
// Writer and Reader can be nil for environments without metrics collection.
func NewHTTPMetrics(log *slog.Logger, writer *VictoriaMetricsWriter, reader *VictoriaMetricsReader) *HTTPMetrics {
	return &HTTPMetrics{
		Log:    log,
		Writer: writer,
		Reader: reader,
	}
}

func (h *HTTPMetrics) Setup() error {
	// For VictoriaMetrics, we don't need to create tables/schemas
	// The metrics are created dynamically when first written
	h.counters = make(map[string]*counterState)
	h.pathBound = 0
	h.candidates = make(map[string]uint64, maxCandidates)

	// Generate unique instance ID using ULID
	h.instance = ulid.MustNew(ulid.Now(), rand.Reader).String()

	h.Log.Info("HTTP metrics initialized with VictoriaMetrics backend", "instance", h.instance)
	return nil
}

// HTTPRequest represents a single HTTP request for metrics
type HTTPRequest struct {
	Timestamp    time.Time
	App          string
	Method       string
	Path         string
	StatusCode   int
	DurationMs   int64
	ResponseSize int64
}

// RecordRequest records an HTTP request as metrics in VictoriaMetrics.
//
// Only maxPathBoundCounters counters are kept for real paths. Anything beyond
// that is still counted, but reported with path and method "other", so an app
// serving unique URLs -- or a client walking random ones -- costs a fixed
// amount of memory here and a fixed number of series downstream.
func (h *HTTPMetrics) RecordRequest(ctx context.Context, req HTTPRequest) error {
	if h == nil || h.Writer == nil {
		return nil
	}

	status := strconv.Itoa(req.StatusCode)
	method, path := req.Method, truncatePath(req.Path)

	h.mu.Lock()

	if h.counters == nil {
		h.counters = make(map[string]*counterState)
	}

	// One entry per path now, so a request either gets tracked with everything
	// it carries or with nothing. Labels follow whichever counter it landed in,
	// or the exported series would name a path the memory bound just refused.
	c, tracked := h.trackCounter(counterKey(req.App, method, path))
	if !tracked {
		method, path = overflowLabel, overflowLabel
		c = h.overflowCounter(counterKey(req.App, method, path))
	}
	if c.app == "" {
		c.app, c.method, c.path = req.App, method, path
	}
	c.observe(status, req.StatusCode, req.DurationMs, h.clock())

	requestCount := c.requests[status]
	durationSum := c.durationSum
	durationCount := c.durationCount

	h.mu.Unlock()

	reqMethod, reqPath := method, path
	durMethod, durPath := method, path

	// Write cumulative counter values and individual duration sample
	points := []MetricPoint{
		{
			Name: "http_requests_total",
			Labels: map[string]string{
				"app":      req.App,
				"method":   reqMethod,
				"path":     reqPath,
				"status":   status,
				"instance": h.instance,
			},
			Value:     requestCount,
			Timestamp: req.Timestamp,
		},
		{
			Name: "http_request_duration_seconds_sum",
			Labels: map[string]string{
				"app":      req.App,
				"method":   durMethod,
				"path":     durPath,
				"instance": h.instance,
			},
			Value:     durationSum,
			Timestamp: req.Timestamp,
		},
		{
			Name: "http_request_duration_seconds_count",
			Labels: map[string]string{
				"app":      req.App,
				"method":   durMethod,
				"path":     durPath,
				"instance": h.instance,
			},
			Value:     durationCount,
			Timestamp: req.Timestamp,
		},
		{
			Name: "http_request_duration_seconds",
			Labels: map[string]string{
				"app":      req.App,
				"method":   durMethod,
				"path":     durPath,
				"instance": h.instance,
			},
			Value:     float64(req.DurationMs) / 1000.0,
			Timestamp: req.Timestamp,
		},
	}

	return h.Writer.WritePoints(ctx, points)
}

// counterKey identifies one app/method/path. The separator is NUL rather than
// ":" because a path may contain a colon: with ":" the old request key for
// ("/x", status 200) and duration key for path "/x:200" were the same string.
func counterKey(app, method, path string) string {
	return app + "\x00" + method + "\x00" + path
}

// truncatePath bounds the client-chosen part of a key. Truncating rather than
// hashing keeps the label readable, and a path this long is already not a route
// anyone is reading off a dashboard.
func truncatePath(path string) string {
	if len(path) <= maxKeyPathBytes {
		return path
	}
	return path[:maxKeyPathBytes] + "..."
}

// clock is time.Now unless a test replaced it.
func (h *HTTPMetrics) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// trackCounter returns the counter for key, admitting it while there is room
// under the cap. When there is not, it records the key as a promotion candidate
// and reports false, and the caller falls back to the overflow entry.
//
// Callers must hold h.mu.
func (h *HTTPMetrics) trackCounter(key string) (*counterState, bool) {
	h.touches++
	if h.touches%promotionWindow == 0 {
		h.promoteCandidate()
	}

	if c, ok := h.counters[key]; ok {
		c.hits++
		return c, true
	}

	if h.pathBound < maxPathBoundCounters {
		c := &counterState{hits: 1, pathBound: true}
		h.counters[key] = c
		h.pathBound++
		return c, true
	}

	h.noteCandidate(key)
	return nil, false
}

// overflowCounter returns the entry untracked traffic accumulates into. These
// are exempt from the cap because their labels carry no client input: how many
// exist is decided by how many apps and status codes there are.
//
// Callers must hold h.mu.
func (h *HTTPMetrics) overflowCounter(key string) *counterState {
	c, ok := h.counters[key]
	if !ok {
		c = &counterState{}
		h.counters[key] = c
	}
	return c
}

// noteCandidate counts a key that missed the cap, so promoteCandidate can see
// which untracked path is actually busy. Callers must hold h.mu.
func (h *HTTPMetrics) noteCandidate(key string) {
	if h.candidates == nil {
		h.candidates = make(map[string]uint64, maxCandidates)
	}
	if _, seen := h.candidates[key]; !seen && len(h.candidates) >= maxCandidates {
		// Full. Keep counting what is already here rather than making room:
		// evicting to admit would let a scan cycle the table endlessly, which
		// is the behaviour the window is being kept away from cardinality to
		// avoid in the first place.
		return
	}
	h.candidates[key]++
}

// promoteCandidate hands the busiest candidate the slot of the least busy
// tracked entry, if it has out-earned it, then decays the survivors and starts
// a fresh window.
//
// One promotion per window, deliberately. A promoted key starts counting from
// zero, since the requests it saw while a candidate were already reported under
// the overflow entry, and a demoted key loses its total outright. Both read
// downstream as a counter reset, so swapping several at once would turn one
// busy window into a wave of resets across every dashboard.
//
// Callers must hold h.mu.
func (h *HTTPMetrics) promoteCandidate() {
	defer clear(h.candidates)

	var (
		bestKey  string
		bestHits uint64
	)
	for key, hits := range h.candidates {
		if hits > bestHits {
			bestKey, bestHits = key, hits
		}
	}

	var (
		weakestKey  string
		weakestHits uint64
	)
	for key, c := range h.counters {
		if !c.pathBound {
			continue
		}
		if weakestKey == "" || c.hits < weakestHits {
			weakestKey, weakestHits = key, c.hits
		}
	}

	// The insert below has to create a slot, not land on one. Overwriting an
	// existing entry would leave pathBound counting a slot that no longer
	// exists, and the effective cap would drift down every time it happened.
	// A candidate is by definition a key that missed the counter map, so this
	// only guards against a collision with an overflow key.
	_, occupied := h.counters[bestKey]

	if bestKey != "" && weakestKey != "" && !occupied &&
		bestHits >= minPromotionHits && bestHits > weakestHits {
		delete(h.counters, weakestKey)
		h.counters[bestKey] = &counterState{hits: bestHits, pathBound: true}
	}

	// Halve rather than reset. A genuinely busy path keeps its lead across
	// windows, while one that has gone quiet falls far enough to be displaced
	// within a few of them.
	for _, c := range h.counters {
		if c.pathBound {
			c.hits /= 2
		}
	}
}

// RPSLastMinute returns requests per second for the last minute
func (h *HTTPMetrics) RPSLastMinute(app string) (float64, error) {
	if h.Reader == nil {
		return 0, fmt.Errorf("reader not initialized")
	}

	query := fmt.Sprintf(`sum(rate(http_requests_total{app="%s"}[1m]))`, app)
	result, err := h.Reader.InstantQuery(context.Background(), query, time.Time{})
	if err != nil {
		return 0, fmt.Errorf("failed to query RPS: %w", err)
	}

	if len(result.Data.Result) == 0 {
		return 0, nil
	}

	valueStr, ok := result.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type")
	}

	rps, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse RPS value: %w", err)
	}

	return rps, nil
}

// RequestStats represents aggregated request statistics
type RequestStats struct {
	Time          time.Time
	Count         int64
	AvgDurationMs float64
	P95DurationMs float64
	P99DurationMs float64
	ErrorRate     float64
}

// StatsLastHour returns request statistics for the last hour in 1-minute buckets
func (h *HTTPMetrics) StatsLastHour(app string) ([]RequestStats, error) {
	if h.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	// Align to minute boundaries for predictable evaluation points
	now := time.Now()
	end := time.Unix(now.Unix()/60*60, 0)
	start := end.Add(-1 * time.Hour)

	// Query for count per minute
	countQuery := fmt.Sprintf(`sum(increase(http_requests_total{app="%s"}[1m]))`, app)
	countResult, err := h.Reader.RangeQuery(context.Background(), countQuery, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query count: %w", err)
	}

	// Query for average duration in milliseconds
	avgQuery := fmt.Sprintf(`(sum(rate(http_request_duration_seconds_sum{app="%s"}[1m])) / sum(rate(http_request_duration_seconds_count{app="%s"}[1m]))) * 1000`, app, app)
	avgResult, err := h.Reader.RangeQuery(context.Background(), avgQuery, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query avg duration: %w", err)
	}

	// Query for p95 duration in milliseconds
	p95Query := fmt.Sprintf(`quantile_over_time(0.95, http_request_duration_seconds{app="%s"}[1m]) * 1000`, app)
	p95Result, err := h.Reader.RangeQuery(context.Background(), p95Query, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query p95: %w", err)
	}

	// Query for p99 duration in milliseconds
	p99Query := fmt.Sprintf(`quantile_over_time(0.99, http_request_duration_seconds{app="%s"}[1m]) * 1000`, app)
	p99Result, err := h.Reader.RangeQuery(context.Background(), p99Query, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query p99: %w", err)
	}

	// Query for error rate
	errorQuery := fmt.Sprintf(`sum(rate(http_requests_total{app="%s",status=~"[45].."}[1m])) / sum(rate(http_requests_total{app="%s"}[1m]))`, app, app)
	errorResult, err := h.Reader.RangeQuery(context.Background(), errorQuery, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query error rate: %w", err)
	}

	// Combine results
	stats := make([]RequestStats, 0)
	if len(countResult.Data.Result) > 0 {
		for _, value := range countResult.Data.Result[0].Values {
			timestamp, _ := value[0].(float64)
			countStr, _ := value[1].(string)
			count, _ := strconv.ParseInt(countStr, 10, 64)

			// Shift timestamp back 1 minute to represent bucket start time
			// (VictoriaMetrics returns the end of the measurement window)
			stat := RequestStats{
				Time:  time.Unix(int64(timestamp), 0).Add(-1 * time.Minute),
				Count: count,
			}

			// Find corresponding values from other queries
			if len(avgResult.Data.Result) > 0 {
				for _, v := range avgResult.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						avgStr, _ := v[1].(string)
						stat.AvgDurationMs, _ = strconv.ParseFloat(avgStr, 64)
						break
					}
				}
			}

			if len(p95Result.Data.Result) > 0 {
				for _, v := range p95Result.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						p95Str, _ := v[1].(string)
						stat.P95DurationMs, _ = strconv.ParseFloat(p95Str, 64)
						break
					}
				}
			}

			if len(p99Result.Data.Result) > 0 {
				for _, v := range p99Result.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						p99Str, _ := v[1].(string)
						stat.P99DurationMs, _ = strconv.ParseFloat(p99Str, 64)
						break
					}
				}
			}

			if len(errorResult.Data.Result) > 0 {
				for _, v := range errorResult.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						errStr, _ := v[1].(string)
						stat.ErrorRate, _ = strconv.ParseFloat(errStr, 64)
						break
					}
				}
			}

			stats = append(stats, stat)
		}
	}

	return stats, nil
}

// PathStats represents statistics for a specific path
type PathStats struct {
	Path          string
	Count         int64
	AvgDurationMs float64
	ErrorRate     float64
}

// TopPaths returns the busiest paths for an app over the last recentMinutes.
//
// Served from the in-process counters rather than from VictoriaMetrics. The
// query it replaced asked for topk by path and then issued two more queries per
// path for the average duration and the error rate, all of them reading the
// `path` label -- the same label the cardinality cap collapses to "other". So
// the cap and this view could not both be right. Reading the counters directly
// removes the conflict, and removes 1+2N round trips per `miren app status`.
//
// Only this node's ingress traffic is counted. That is the whole cluster today,
// since the ingress runs on the coordinator alone; it stops being true the day
// runners serve ingress, and this has to aggregate across them then.
func (h *HTTPMetrics) TopPaths(app string, limit int) ([]PathStats, error) {
	if limit <= 0 {
		return nil, nil
	}

	now := h.clock()

	h.mu.Lock()
	var paths []PathStats
	for _, c := range h.counters {
		// Overflow entries are skipped: "other" is not a path anyone can act
		// on, and it would outrank every real one on any busy app.
		if !c.pathBound || c.app != app {
			continue
		}

		w := c.window(now)
		if w.count == 0 {
			continue
		}

		stats := PathStats{
			Path:  c.path,
			Count: w.count,
		}
		if w.durationCount > 0 {
			stats.AvgDurationMs = (w.durationSum / float64(w.durationCount)) * 1000.0
		}
		stats.ErrorRate = float64(w.errors) / float64(w.count)
		paths = append(paths, stats)
	}
	h.mu.Unlock()

	slices.SortFunc(paths, func(a, b PathStats) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.Path, b.Path) // stable across calls, unlike map order
	})

	if len(paths) > limit {
		paths = paths[:limit]
	}
	return paths, nil
}

// ErrorBreakdown represents error counts by status code
type ErrorBreakdown struct {
	StatusCode int
	Count      int64
	Percentage float64
}

// ErrorsLastHour returns breakdown of errors by status code for the last hour
func (h *HTTPMetrics) ErrorsLastHour(app string) ([]ErrorBreakdown, error) {
	if h.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	// Query for errors grouped by status code
	query := fmt.Sprintf(`sum by(status) (increase(http_requests_total{app="%s",status=~"[45].."}[1h]))`, app)
	result, err := h.Reader.InstantQuery(context.Background(), query, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("failed to query errors: %w", err)
	}

	var errors []ErrorBreakdown
	var totalErrors int64

	for _, r := range result.Data.Result {
		statusStr := r.Metric["status"]
		status, _ := strconv.Atoi(statusStr)
		countStr, _ := r.Value[1].(string)
		count, _ := strconv.ParseInt(countStr, 10, 64)

		totalErrors += count
		errors = append(errors, ErrorBreakdown{
			StatusCode: status,
			Count:      count,
		})
	}

	// Calculate percentages
	if totalErrors > 0 {
		for i := range errors {
			errors[i].Percentage = float64(errors[i].Count) / float64(totalErrors) * 100
		}
	}

	return errors, nil
}

// Close is a no-op for VictoriaMetrics (writer handles its own lifecycle)
func (h *HTTPMetrics) Close() error {
	return nil
}
