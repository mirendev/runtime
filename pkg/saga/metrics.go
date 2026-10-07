package saga

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/metrics"
)

// CountsMetrics publishes a process's saga Counts as counters, so an operator
// can see sagas failing to finish without reading every node's log.
//
// Every executor in the process counts into the same set, so one collector per
// process covers sandbox, build and addon definitions alike. Counters from
// different processes cannot share a series, which is what Entity is for: the
// coordinator and each distributed runner push under their own.
//
// A definition's series appear once anything has been counted for it. See Emit
// for how its first push keeps the counts behind it from being invisible.
type CountsMetrics struct {
	Log    *slog.Logger
	Writer metrics.PointWriter
	Counts *Counts

	// Entity is the value of the "entity" label on every emitted series.
	Entity string

	mu sync.Mutex
	// baselined names the definitions whose zero baseline has been pushed.
	baselined map[string]bool
}

const defaultCountsMetricsInterval = 10 * time.Second

// NewCountsMetrics creates a collector for DefaultCounts under the control
// process's entity. Writer may be nil for environments without metrics
// collection, in which case Monitor is a no-op.
func NewCountsMetrics(log *slog.Logger, writer metrics.PointWriter) *CountsMetrics {
	return &CountsMetrics{
		Log:    log,
		Writer: writer,
		Counts: DefaultCounts,
		Entity: metrics.EntityControl,
	}
}

// Monitor pushes the counters every defaultCountsMetricsInterval until ctx is
// cancelled.
func (m *CountsMetrics) Monitor(ctx context.Context) {
	if m.Writer == nil {
		return
	}

	ticker := time.NewTicker(defaultCountsMetricsInterval)
	defer ticker.Stop()

	for {
		if err := m.Emit(ctx, time.Now()); err != nil {
			m.Log.Debug("failed to record saga counts", "entity", m.Entity, "err", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// ResendBaselines makes the next push carry every definition's zero baseline
// again. A sink attached to the writer after the first push never saw them,
// and the coordinator's shipping sink is attached exactly that late.
func (m *CountsMetrics) ResendBaselines() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baselined = nil
}

// countSeries maps each Event to the series and outcome it is published as.
// Events that are two ends of one thing share a series and differ by outcome,
// so a ratio is one query.
var countSeries = [numEvents]struct{ name, outcome string }{
	EventStarted:            {"saga_executions_started_total", ""},
	EventCompleted:          {"saga_executions_finished_total", "completed"},
	EventRolledBack:         {"saga_executions_finished_total", "rolled_back"},
	EventCompensationFailed: {"saga_compensation_failures_total", ""},
	EventRecovered:          {"saga_recoveries_total", "recovered"},
	EventRecoveryFailed:     {"saga_recoveries_total", "failed"},
	EventStrandedForced:     {"saga_stranded_forced_total", ""},
}

// Emit pushes one sample of every counter.
//
// The first push of a definition also carries a zero for each of its series,
// stamped at the moment the definition was first counted, which is when the
// counts really were zero. Without it the counts behind that first push would
// be invisible to increase() on a brand-new series, and the one most likely to
// be brand new is the one an alert cares about: recovery runs at boot, before
// anything else has been counted, so a failed recovery is often the first
// sample its series ever gets.
func (m *CountsMetrics) Emit(ctx context.Context, now time.Time) error {
	snap := m.Counts.Snapshot()
	if len(snap) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var baselined []string
	points := make([]metrics.MetricPoint, 0, len(snap)*NumEvents)
	for def, counts := range snap {
		baseline := !m.baselined[def]
		if baseline {
			baselined = append(baselined, def)
		}
		// Strictly before now, so the two samples never collide.
		since := counts.Since
		if !since.Before(now) {
			since = now.Add(-time.Millisecond)
		}
		for ev, series := range countSeries {
			labels := map[string]string{
				"entity":     m.Entity,
				"definition": def,
			}
			if series.outcome != "" {
				labels["outcome"] = series.outcome
			}
			if baseline {
				points = append(points, metrics.MetricPoint{
					Name: series.name, Labels: labels, Value: 0, Timestamp: since,
				})
			}
			points = append(points, metrics.MetricPoint{
				Name:      series.name,
				Labels:    labels,
				Value:     float64(counts.Events[ev]),
				Timestamp: now,
			})
		}
	}
	if err := m.Writer.WritePoints(ctx, points); err != nil {
		// Not marked, so the next push carries the baseline again.
		return err
	}

	if m.baselined == nil {
		m.baselined = make(map[string]bool)
	}
	for _, def := range baselined {
		m.baselined[def] = true
	}
	return nil
}

// InFlightMetrics publishes how many executions are in flight cluster-wide and
// how long the oldest has been running, by definition and status.
//
// It reports facts and leaves "stuck" to whoever reads them. How long is too
// long differs by definition (a build legitimately outlasts a sandbox create),
// and the threshold belongs in the alert rule, where it can be tuned without a
// release.
//
// Age is measured from when an execution started, not when it last changed. An
// execution retrying a failing undo saves on every attempt, so its last change
// stays fresh however long it has been stuck, and that is exactly the one this
// gauge exists to show.
//
// The in-flight set is shared by the whole cluster, so this runs on the
// coordinator only. It walks the set every walkInterval and pushes the last
// walk's result more often than that, computing ages at push time, so the
// series stay live without paying for a walk on every push. A result too old
// to trust is not pushed at all; see Emit.
type InFlightMetrics struct {
	Log     *slog.Logger
	Writer  metrics.PointWriter
	Storage InFlightStorage

	mu sync.Mutex
	// groups is the last walk's result, nil until a walk has finished.
	groups map[inFlightKey]inFlightGroup
	// lastWalk is when groups was taken.
	lastWalk time.Time
	// nextWalk is when the next walk is due. It counts from when the last
	// attempt ended, so a slow walk is followed by a full interval of rest
	// rather than by another walk straight away.
	nextWalk time.Time
	// pushed is every label set this process has pushed, so one whose
	// executions have all finished can be written once at zero rather than
	// left reading its last value until the store's lookback expires.
	pushed map[inFlightKey]bool
}

// InFlightStorage is the one read InFlightMetrics needs.
type InFlightStorage interface {
	ListIncompleteSummaryPage(ctx context.Context, q IncompleteSummaryQuery) (*IncompleteSummaryPage, error)
}

// The walk reads every in-flight execution from the store, whole entities and
// all, so it runs far less often than the push. Ages are computed at push time,
// which leaves the count as the only thing that lags, and five minutes of lag
// on a count is nothing next to the thresholds anyone alerts on.
const (
	defaultInFlightPushInterval = 30 * time.Second
	defaultInFlightWalkInterval = 5 * time.Minute

	// inFlightWalkTimeout bounds one walk. On a cluster still draining a large
	// stranded backlog a walk can be slow, and an unbounded one would hold the
	// store's attention for as long as it took.
	inFlightWalkTimeout = 2 * time.Minute

	// inFlightMaxResultAge is how long a walk's result is pushed for when
	// the walks after it keep failing: three missed walks.
	inFlightMaxResultAge = 3 * defaultInFlightWalkInterval
)

type inFlightKey struct {
	definition string
	status     Status
}

type inFlightGroup struct {
	count int
	// oldest is the earliest start in the group, zero if no execution in it
	// carries a usable start time.
	oldest time.Time
}

// NewInFlightMetrics creates an in-flight collector. Writer may be nil for
// environments without metrics collection, in which case Monitor is a no-op.
func NewInFlightMetrics(log *slog.Logger, writer metrics.PointWriter, storage InFlightStorage) *InFlightMetrics {
	return &InFlightMetrics{Log: log, Writer: writer, Storage: storage}
}

// Monitor walks and pushes until ctx is cancelled.
func (m *InFlightMetrics) Monitor(ctx context.Context) {
	if m.Writer == nil || m.Storage == nil {
		return
	}

	ticker := time.NewTicker(defaultInFlightPushInterval)
	defer ticker.Stop()

	for {
		if m.walkDue(time.Now()) {
			// A failed walk keeps the previous result rather than pushing
			// nothing: the store not answering is not every execution
			// finishing. Emit stops pushing it once it is too old to trust.
			walkCtx, cancel := context.WithTimeout(ctx, inFlightWalkTimeout)
			err := m.Walk(walkCtx, time.Now())
			cancel()
			if err != nil {
				m.Log.Debug("failed to walk in-flight sagas", "err", err)
				m.walkFailed(time.Now())
			}
		}
		if err := m.Emit(ctx, time.Now()); err != nil {
			m.Log.Debug("failed to record in-flight sagas", "err", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (m *InFlightMetrics) walkDue(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !now.Before(m.nextWalk)
}

// walkFailed schedules the next walk after a failed one. A store that is not
// answering gets the same rest as one that answered.
func (m *InFlightMetrics) walkFailed(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextWalk = now.Add(defaultInFlightWalkInterval)
}

// Walk reads the whole in-flight set and keeps a count and oldest start per
// definition and status. A walk that does not finish changes nothing, because
// half a walk would report the rest of the set as having finished.
//
// The walk covers one status index after another, and a stale entry in an
// earlier index names an execution that has since moved to a later one. The
// summary carries its real status, so it is not miscounted, but it is reached
// twice. Each execution is counted once, by ID.
func (m *InFlightMetrics) Walk(ctx context.Context, now time.Time) error {
	groups := make(map[inFlightKey]inFlightGroup)
	seen := make(map[string]struct{})

	cursor := ""
	for {
		page, err := m.Storage.ListIncompleteSummaryPage(ctx, IncompleteSummaryQuery{Cursor: cursor})
		if err != nil {
			return err
		}
		for _, s := range page.Executions {
			if _, dup := seen[s.ID]; dup {
				continue
			}
			seen[s.ID] = struct{}{}
			key := inFlightKey{definition: s.DefinitionName, status: s.Status}
			g := groups[key]
			g.count++
			if !s.CreatedAt.IsZero() && (g.oldest.IsZero() || s.CreatedAt.Before(g.oldest)) {
				g.oldest = s.CreatedAt
			}
			groups[key] = g
		}
		cursor = page.Cursor
		if cursor == "" {
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups = groups
	m.lastWalk = now
	m.nextWalk = time.Now().Add(defaultInFlightWalkInterval)
	return nil
}

// Emit pushes the last walk's result, with ages as of now. A label set pushed
// before but absent from the last walk is written at zero.
//
// Once the last good walk is older than inFlightMaxResultAge, Emit pushes
// nothing and the series go stale. Pushing it anyway would stamp an old picture
// with a fresh timestamp and a growing age, and an execution that finished
// after the store stopped answering would set off an age alert. Stale series
// are the truthful signal here: nobody knows what is in flight.
func (m *InFlightMetrics) Emit(ctx context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.groups == nil || now.Sub(m.lastWalk) > inFlightMaxResultAge {
		return nil
	}

	var points []metrics.MetricPoint
	add := func(key inFlightKey, count int, age time.Duration) {
		labels := map[string]string{"definition": key.definition, "status": string(key.status)}
		points = append(points,
			metrics.MetricPoint{Name: "saga_incomplete_executions", Labels: labels, Value: float64(count), Timestamp: now},
			metrics.MetricPoint{Name: "saga_incomplete_oldest_age_seconds", Labels: labels, Value: age.Seconds(), Timestamp: now},
		)
	}

	for key, g := range m.groups {
		var age time.Duration
		if !g.oldest.IsZero() && now.After(g.oldest) {
			age = now.Sub(g.oldest)
		}
		add(key, g.count, age)
	}
	for key := range m.pushed {
		if _, ok := m.groups[key]; !ok {
			add(key, 0, 0)
		}
	}

	if len(points) == 0 {
		return nil
	}
	if err := m.Writer.WritePoints(ctx, points); err != nil {
		// Keep pushed as it was so the next push retries the zeros.
		return err
	}

	// Only what this push actually reported stays tracked: a retired label set
	// has been written at zero and needs nothing more.
	m.pushed = make(map[inFlightKey]bool, len(m.groups))
	for key := range m.groups {
		m.pushed[key] = true
	}
	return nil
}
