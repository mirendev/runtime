// Package usage answers cluster-wide resource questions: which sandboxes are
// consuming what, and which hosts are under pressure.
//
// It reads from VictoriaMetrics rather than probing runners directly. Every
// runner already ships its sandbox metrics through the coordinator into one
// store, so a single query here sees the whole cluster with no fan-out, no
// per-node timeout budget, and no partial answer when a runner is unreachable.
// It also means a window can be historical: the spike that caused the page an
// hour ago is still there to be found, which no live probe could offer.
package usage

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/rpc/standard"
)

// Server answers resource usage queries. It belongs on the coordinator, which
// is the only process holding both the entity store and a reader for the
// cluster's metrics.
type Server struct {
	Log *slog.Logger

	// EC resolves what a sandbox is: which app, service and node explain it.
	EC *entityserver.Client

	// Reader resolves what a sandbox is doing. Nil disables usage figures
	// without disabling the listing, so a cluster with no metrics backend still
	// answers "what is running where" instead of failing outright.
	Reader *metrics.VictoriaMetricsReader
}

func NewServer(log *slog.Logger, ec *entityserver.Client, reader *metrics.VictoriaMetricsReader) *Server {
	return &Server{Log: log, EC: ec, Reader: reader}
}

var _ usage_v1alpha.ResourceUsage = (*Server)(nil)

// window is a resolved measurement span.
type window struct {
	start     time.Time
	end       time.Time
	aggregate string
}

func (w window) duration() time.Duration {
	d := w.end.Sub(w.start)
	if d <= 0 {
		return defaultWindow
	}
	return d
}

func (w window) encode() *usage_v1alpha.UsageWindow {
	var uw usage_v1alpha.UsageWindow
	uw.SetStart(standard.ToTimestamp(w.start))
	uw.SetEnd(standard.ToTimestamp(w.end))
	uw.SetAggregate(w.aggregate)
	return &uw
}

// resolveWindow turns a caller's bounds into a measured span.
//
// Both ends are optional and independent: an unset end means now, an unset (or
// nonsensical) start means one minute before the end. There is exactly one way
// to express a window, so there is no precedence rule to learn.
func resolveWindow(start, end *standard.Timestamp, aggregate string) window {
	endAt := time.Now()
	if end != nil {
		if t := standard.FromTimestamp(end); !t.IsZero() {
			endAt = t
		}
	}

	var startAt time.Time
	if start != nil {
		startAt = standard.FromTimestamp(start)
	}

	if startAt.IsZero() || !startAt.Before(endAt) {
		startAt = endAt.Add(-defaultWindow)
	}

	return window{start: startAt, end: endAt, aggregate: resolveAggregate(aggregate)}
}

// sandboxListing is the answer both surfaces return, before either has shaped
// it into its own result type.
type sandboxListing struct {
	rows     []*usage_v1alpha.SandboxUsage
	cluster  totals
	total    int32
	warnings []string
}

// listSandboxes is the one implementation behind both ListSandboxes (RPC) and
// HttpListSandboxes (REST). Neither surface holds logic of its own beyond
// translating its arguments.
func (s *Server) listSandboxes(ctx context.Context, f filter, w window, ord ordering) (*sandboxListing, error) {
	dir, err := s.loadDirectory(ctx, f)
	if err != nil {
		return nil, err
	}

	out := &sandboxListing{}

	cpu, memory, nodeCores, warnings := s.sandboxSamples(ctx, f, w, dir)
	out.warnings = warnings

	rows := make([]*usage_v1alpha.SandboxUsage, 0, len(dir.sandboxes))

	for _, sb := range dir.sandboxes {
		if !f.matches(sb.ref) {
			continue
		}

		id := sb.ref.Sandbox()
		cores, haveCPU := cpu[id]
		bytes, haveMem := memory[id]

		var row usage_v1alpha.SandboxUsage
		row.SetRef(sb.ref)
		row.SetMeasuredAt(standard.ToTimestamp(w.end))

		// A sandbox with no samples is reported as a row with blank usage and
		// stale set, never dropped. A sandbox that is running but has stopped
		// reporting is a finding in itself; omitting it would make a broken
		// telemetry pipeline look like an idle cluster.
		row.SetStale(!haveCPU && !haveMem)

		var c usage_v1alpha.CpuUsage
		c.SetCores(cores)
		if total := nodeCores[sb.ref.Node()]; total > 0 {
			c.SetPercentOfNode(cores / total * 100)
		}
		row.SetCpu(&c)

		var m usage_v1alpha.MemoryUsage
		m.SetBytes(int64(bytes))
		row.SetMemory(&m)

		out.cluster.cpuCores += cores
		out.cluster.memoryBytes += int64(bytes)

		rows = append(rows, &row)
	}

	out.total = int32(len(rows))
	sortSandboxes(rows, ord)
	out.rows = rows[:ord.truncate(len(rows))]

	return out, nil
}

// ListSandboxes is the RPC surface: grouped arguments, no HTTP binding.
func (s *Server) ListSandboxes(ctx context.Context, state *usage_v1alpha.ResourceUsageListSandboxes) error {
	args := state.Args()

	// Resolved once. An unset end means now, so calling windowFrom again for
	// the response would resolve a later instant than the one the metrics were
	// queried at, and the reported window would not be the measured one.
	w := windowFrom(args.Window())

	listing, err := s.listSandboxes(ctx, selectorToFilter(args.Selector()), w, orderingFrom(args.Ordering()))
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetSandboxes(listing.rows)
	res.SetWindow(w.encode())
	res.SetCluster(listing.cluster.encode())
	res.SetTotalCount(listing.total)
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// HttpListSandboxes serves GET /api/v1/usage/sandboxes with flat query
// parameters.
func (s *Server) HttpListSandboxes(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpListSandboxes) error {
	args := state.Args()

	f := filter{
		app:           args.App(),
		service:       args.Service(),
		node:          args.Node(),
		kind:          args.Kind(),
		status:        args.Status(),
		includeSystem: args.IncludeSystem(),
		includeAddons: true,
	}
	w := restWindow(args.Since(), args.Until(), args.Aggregate())
	ord := ordering{sort: args.Sort(), order: args.Order(), limit: int(args.Limit())}

	listing, err := s.listSandboxes(ctx, f, w, ord)
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetSandboxes(listing.rows)
	res.SetWindow(w.encode())
	res.SetCluster(listing.cluster.encode())
	res.SetTotalCount(listing.total)
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// sandboxSamples runs the metric queries behind a listing.
//
// Two queries answer the whole cluster, not two per sandbox: grouping by
// sandbox in the query means the cost of this call does not grow with the
// number of sandboxes, which is what makes a refreshing view affordable.
func (s *Server) sandboxSamples(
	ctx context.Context,
	f filter,
	w window,
	dir *directory,
) (cpu, memory, nodeCores map[string]float64, warnings []string) {
	cpu = map[string]float64{}
	memory = map[string]float64{}
	nodeCores = map[string]float64{}

	if s.Reader == nil {
		return cpu, memory, nodeCores, []string{"no metrics backend configured; usage figures are unavailable"}
	}

	selector := ""
	if f.node != "" {
		if n := matchNode(dir.nodes, f.node); n != nil {
			selector = labelSelector(map[string]string{labelNode: string(n.id)})
		}
	}

	dur := w.duration()

	cpuQ := cpuCoresQuery(labelSandbox, selector, dur, w.aggregate)
	if vals, err := s.instantByLabel(ctx, cpuQ, labelSandbox, w.end); err != nil {
		warnings = append(warnings, "cpu usage unavailable: "+err.Error())
	} else {
		cpu = vals
	}

	memQ := memoryBytesQuery(labelSandbox, selector, dur, w.aggregate)
	if vals, err := s.instantByLabel(ctx, memQ, labelSandbox, w.end); err != nil {
		warnings = append(warnings, "memory usage unavailable: "+err.Error())
	} else {
		memory = vals
	}

	// Node capacity is what turns "2 cores" into "half the machine". Its
	// absence degrades a column rather than the answer, so a failure here is a
	// warning and the percentages simply stay zero.
	capQ := nodeGaugeQuery(metricNodeCPUCoresTotal, selector, dur, aggregateLast)
	if vals, err := s.instantByLabel(ctx, capQ, labelNode, w.end); err != nil {
		warnings = append(warnings, "node capacity unavailable: "+err.Error())
	} else {
		nodeCores = vals
	}

	return cpu, memory, nodeCores, warnings
}

// instantByLabel runs one instant query and indexes the result by a label.
func (s *Server) instantByLabel(ctx context.Context, query, label string, at time.Time) (map[string]float64, error) {
	result, err := s.Reader.InstantQuery(ctx, query, at)
	if err != nil {
		return nil, err
	}

	out := make(map[string]float64, len(result.Data.Result))
	for _, r := range result.Data.Result {
		key := r.Metric[label]
		if key == "" || len(r.Value) < 2 {
			continue
		}
		raw, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		out[key] = v
	}

	return out, nil
}

// totals accumulates a rolled-up slice of usage.
type totals struct {
	cpuCores    float64
	memoryBytes int64
	cpuPercent  float64
	memPercent  float64
}

func (t totals) encode() *usage_v1alpha.ResourceTotals {
	var rt usage_v1alpha.ResourceTotals
	rt.SetCpuCores(t.cpuCores)
	rt.SetCpuPercent(t.cpuPercent)
	rt.SetMemoryBytes(t.memoryBytes)
	rt.SetMemoryPercent(t.memPercent)
	return &rt
}

// sortSandboxes orders rows server-side. This has to happen here rather than in
// the client because a limit is only meaningful against a known order: taking
// the first 20 of an unsorted listing returns 20 arbitrary sandboxes, not the
// 20 busiest.
// sandboxName is what a person calls a sandbox: the short id they type and see
// in a listing, falling back to the full entity id for one too old to have one.
func sandboxName(ref *usage_v1alpha.SandboxRef) string {
	if short := ref.SandboxShortId(); short != "" {
		return short
	}
	return ref.Sandbox()
}

func sortSandboxes(rows []*usage_v1alpha.SandboxUsage, ord ordering) {
	// Each key states the direction that reads naturally for it, so a name
	// never inherits a usage column's busiest-first default.
	var (
		less      func(a, b *usage_v1alpha.SandboxUsage) bool
		ascending bool
	)

	switch strings.ToLower(strings.TrimSpace(ord.sort)) {
	case "memory", "mem":
		less = func(a, b *usage_v1alpha.SandboxUsage) bool { return a.Memory().Bytes() < b.Memory().Bytes() }
	case "app":
		less, ascending = func(a, b *usage_v1alpha.SandboxUsage) bool { return a.Ref().App() < b.Ref().App() }, true
	case "service":
		less, ascending = func(a, b *usage_v1alpha.SandboxUsage) bool { return a.Ref().Service() < b.Ref().Service() }, true
	case "node", "runner":
		less, ascending = func(a, b *usage_v1alpha.SandboxUsage) bool { return a.Ref().NodeName() < b.Ref().NodeName() }, true
	case "name":
		less, ascending = func(a, b *usage_v1alpha.SandboxUsage) bool { return sandboxName(a.Ref()) < sandboxName(b.Ref()) }, true
	default:
		less = func(a, b *usage_v1alpha.SandboxUsage) bool { return a.Cpu().Cores() < b.Cpu().Cores() }
	}

	desc := ord.direction(ascending)

	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}
