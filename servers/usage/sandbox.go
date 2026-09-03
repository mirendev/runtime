package usage

import (
	"context"
	"fmt"
	"strconv"
	"time"

	computev1 "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
)

// defaultSeriesPoints is how many points a history is rendered at when the
// caller does not choose. It is a target, not a guarantee: the step is derived
// from it so that a one-minute window and a one-day window both come back at a
// resolution a chart can actually draw.
const defaultSeriesPoints = 60

// detailOptions is what a caller wants alongside one sandbox's current usage.
type detailOptions struct {
	series     bool
	containers bool
	step       time.Duration
}

// sandboxDetailResult is what the core produces, before either surface shapes
// it into its own generated result type. Those types are write-only, so the
// core cannot hand one to the other -- it returns plain values instead.
type sandboxDetailResult struct {
	usage         *usage_v1alpha.SandboxUsage
	series        []*usage_v1alpha.UsageSeries
	exit          *usage_v1alpha.SandboxExit
	crashLoop     *usage_v1alpha.CrashLoopState
	image         string
	restartPolicy string
	warnings      []string
}

// apply writes the result into an RPC results type. Both surfaces have the same
// setters, so each passes its own and this stays the only place the mapping
// lives.
func (d *sandboxDetailResult) apply(set sandboxDetailSetter, w window) {
	set.SetUsage(d.usage)
	set.SetSeries(d.series)
	if d.exit != nil {
		set.SetExit(d.exit)
	}
	if d.crashLoop != nil {
		set.SetCrashLoop(d.crashLoop)
	}
	set.SetImage(d.image)
	set.SetRestartPolicy(d.restartPolicy)
	set.SetWindow(w.encode())
	set.SetWarnings(d.warnings)
}

// sandboxDetailSetter is the shape both surfaces' result types share.
type sandboxDetailSetter interface {
	SetUsage(*usage_v1alpha.SandboxUsage)
	SetSeries([]*usage_v1alpha.UsageSeries)
	SetExit(*usage_v1alpha.SandboxExit)
	SetCrashLoop(*usage_v1alpha.CrashLoopState)
	SetImage(string)
	SetRestartPolicy(string)
	SetWindow(*usage_v1alpha.UsageWindow)
	SetWarnings([]string)
}

// sandboxDetail is the one implementation behind GetSandbox (RPC) and
// HttpGetSandbox (REST).
//
// This is the third step of an investigation -- after finding the hot node and
// the hot sandbox on it -- so it answers "why" rather than "how much". The exit
// code and crash-loop state are the point; usage is context.
func (s *Server) sandboxDetail(
	ctx context.Context,
	query string,
	w window,
	opts detailOptions,
) (*sandboxDetailResult, error) {
	if query == "" {
		return nil, fmt.Errorf("a sandbox id is required")
	}

	// Resolve through the same directory the listing uses, so the identity
	// shown here and the identity shown in a row cannot disagree.
	dir, err := s.loadDirectory(ctx, filter{includeSystem: true})
	if err != nil {
		return nil, err
	}

	var found *sandboxRow
	for i := range dir.sandboxes {
		sb := &dir.sandboxes[i]
		if sb.ref.Sandbox() == query || sb.ref.SandboxShortId() == query {
			found = sb
			break
		}
	}

	if found == nil {
		return nil, fmt.Errorf("sandbox %q not found, or it is not running", query)
	}

	out := &sandboxDetailResult{}

	var row usage_v1alpha.SandboxUsage
	row.SetRef(found.ref)
	row.SetMeasuredAt(standard.ToTimestamp(w.end))

	sel := labelSelector(map[string]string{labelSandbox: found.ref.Sandbox()})

	cores, bytes, sampleWarnings := s.singleSandboxSamples(ctx, sel, w)
	out.warnings = append(out.warnings, sampleWarnings...)

	var cpu usage_v1alpha.CpuUsage
	cpu.SetCores(cores)

	// The share of the host is what says whether the absolute figure matters,
	// so it is worth the extra query even for a single sandbox.
	if nodeCores := s.nodeCoreCount(ctx, found.ref.Node(), w); nodeCores > 0 {
		cpu.SetPercentOfNode(cores / nodeCores * 100)
	}

	row.SetCpu(&cpu)

	var mem usage_v1alpha.MemoryUsage
	mem.SetBytes(int64(bytes))
	row.SetMemory(&mem)

	row.SetStale(cores == 0 && bytes == 0)

	if opts.series {
		series, seriesWarnings := s.sandboxSeries(ctx, sel, w, opts.step)
		out.warnings = append(out.warnings, seriesWarnings...)
		out.series = series
	}

	if opts.containers {
		// The stored series sum a sandbox's containers before they are written,
		// so a per-container split cannot be recovered from them. Saying so
		// beats returning an empty list that reads as "one container".
		out.warnings = append(out.warnings,
			"per-container usage is not available from stored metrics; it needs a live probe of the owning node")
	}

	// Exit and crash-loop state come from entities, not metrics: they are facts
	// about what the platform decided, not measurements of the sandbox.
	if err := s.decorateFromEntities(ctx, found, out); err != nil {
		out.warnings = append(out.warnings, "sandbox detail incomplete: "+err.Error())
	}

	out.usage = &row

	return out, nil
}

// GetSandbox is the RPC surface.
func (s *Server) GetSandbox(ctx context.Context, state *usage_v1alpha.ResourceUsageGetSandbox) error {
	args := state.Args()

	opts := detailOptions{}
	if o := args.Options(); o != nil {
		opts.series = o.IncludeSeries()
		opts.containers = o.IncludeContainers()
		opts.step = time.Duration(o.StepSeconds()) * time.Second
	}

	w := windowFrom(args.Window())

	detail, err := s.sandboxDetail(ctx, args.Sandbox(), w, opts)
	if err != nil {
		return err
	}

	detail.apply(state.Results(), w)

	return nil
}

// HttpGetSandbox serves GET /api/v1/usage/sandboxes/{sandbox}.
func (s *Server) HttpGetSandbox(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpGetSandbox) error {
	args := state.Args()

	opts := detailOptions{series: args.Series(), containers: args.Containers()}
	if d, ok := parseRESTDuration(args.Step()); ok {
		opts.step = d
	}

	w := restWindow(args.Since(), args.Until(), args.Aggregate())

	detail, err := s.sandboxDetail(ctx, args.Sandbox(), w, opts)
	if err != nil {
		return err
	}

	detail.apply(state.Results(), w)

	return nil
}

func (s *Server) singleSandboxSamples(ctx context.Context, selector string, w window) (cores, bytes float64, warnings []string) {
	if s.Reader == nil {
		return 0, 0, []string{"no metrics backend configured; usage figures are unavailable"}
	}

	dur := w.duration()

	if vals, err := s.instantByLabel(ctx, cpuCoresQuery(labelSandbox, selector, dur, w.aggregate), labelSandbox, w.end); err != nil {
		warnings = append(warnings, "cpu usage unavailable: "+err.Error())
	} else {
		for _, v := range vals {
			cores = v
		}
	}

	if vals, err := s.instantByLabel(ctx, memoryBytesQuery(labelSandbox, selector, dur, w.aggregate), labelSandbox, w.end); err != nil {
		warnings = append(warnings, "memory usage unavailable: "+err.Error())
	} else {
		for _, v := range vals {
			bytes = v
		}
	}

	return cores, bytes, warnings
}

// sandboxSeries fetches CPU and memory over time for charting.
func (s *Server) sandboxSeries(ctx context.Context, selector string, w window, step time.Duration) ([]*usage_v1alpha.UsageSeries, []string) {
	if s.Reader == nil {
		return nil, nil
	}

	if step <= 0 {
		// Derive a step that yields roughly defaultSeriesPoints points, so the
		// resolution matches the window instead of returning either three
		// points for an hour or thousands for a day.
		step = w.duration() / defaultSeriesPoints
	}
	if step < time.Second {
		step = time.Second
	}

	var out []*usage_v1alpha.UsageSeries
	var warnings []string

	queries := []struct {
		metric string
		query  string
	}{
		{"cpu_cores", cpuCoresQuery(labelSandbox, selector, step, aggregateAvg)},
		{"memory_bytes", memoryBytesQuery(labelSandbox, selector, step, aggregateLast)},
	}

	for _, q := range queries {
		result, err := s.Reader.RangeQuery(ctx, q.query, w.start, w.end, promDuration(step))
		if err != nil {
			warnings = append(warnings, q.metric+" history unavailable: "+err.Error())
			continue
		}

		var series usage_v1alpha.UsageSeries
		series.SetMetric(q.metric)

		var points []*usage_v1alpha.UsagePoint
		for _, r := range result.Data.Result {
			for _, v := range r.Values {
				if len(v) < 2 {
					continue
				}
				ts, _ := v[0].(float64)
				raw, ok := v[1].(string)
				if !ok {
					continue
				}
				val, err := strconv.ParseFloat(raw, 64)
				if err != nil {
					continue
				}

				var p usage_v1alpha.UsagePoint
				p.SetAt(standard.ToTimestamp(time.Unix(int64(ts), 0)))
				p.SetValue(val)
				points = append(points, &p)
			}
		}

		series.SetPoints(points)
		out = append(out, &series)
	}

	return out, warnings
}

// decorateFromEntities adds the facts the metrics store does not hold: how the
// sandbox exited, what image it runs, and whether its pool has given up
// restarting it.
func (s *Server) decorateFromEntities(
	ctx context.Context,
	row *sandboxRow,
	out *sandboxDetailResult,
) error {
	var sb computev1.Sandbox
	if err := s.EC.GetById(ctx, entity.Id(row.ref.Sandbox()), &sb); err != nil {
		return err
	}

	out.restartPolicy = string(sb.Spec.RestartPolicy)

	// The first non-pause container is the one an operator means by "the
	// image"; the pause container is an implementation detail of the sandbox.
	for _, c := range sb.Spec.Container {
		if c.Image != "" {
			out.image = c.Image
			break
		}
	}

	if sb.Exit.At.IsZero() && sb.Exit.Code == 0 && sb.Exit.Container == "" {
		// Never exited; leaving the field unset says that more clearly than a
		// zeroed exit record, which would read as a clean exit.
		return s.decorateCrashLoop(ctx, row, out)
	}

	var exit usage_v1alpha.SandboxExit
	exit.SetCode(int32(sb.Exit.Code))
	exit.SetAt(standard.ToTimestamp(sb.Exit.At))
	exit.SetContainer(sb.Exit.Container)
	out.exit = &exit

	return s.decorateCrashLoop(ctx, row, out)
}

func (s *Server) decorateCrashLoop(
	ctx context.Context,
	row *sandboxRow,
	out *sandboxDetailResult,
) error {
	if row.poolID == "" {
		return nil
	}

	var pool computev1.SandboxPool
	if err := s.EC.GetById(ctx, entity.Id(row.poolID), &pool); err != nil {
		return err
	}

	var cl usage_v1alpha.CrashLoopState
	cl.SetConsecutiveCrashes(int32(pool.ConsecutiveCrashCount))
	cl.SetLastCrashAt(standard.ToTimestamp(pool.LastCrashTime))
	cl.SetCooldownUntil(standard.ToTimestamp(pool.CooldownUntil))
	out.crashLoop = &cl

	return nil
}

// nodeCoreCount reads how many cores the sandbox's host has, returning 0 when
// that is unknown. Zero is the caller's signal to omit the share rather than
// print a percentage of nothing.
func (s *Server) nodeCoreCount(ctx context.Context, node string, w window) float64 {
	if s.Reader == nil || node == "" {
		return 0
	}

	sel := labelSelector(map[string]string{labelNode: node})
	vals, err := s.instantByLabel(ctx, nodeGaugeQuery(metricNodeCPUCoresTotal, sel, w.duration(), aggregateLast), labelNode, w.end)
	if err != nil {
		return 0
	}

	return vals[node]
}
