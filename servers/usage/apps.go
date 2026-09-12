package usage

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/compute"

	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

// appListing is the answer both surfaces return.
type appListing struct {
	rows     []*usage_v1alpha.AppUsage
	cluster  totals
	total    int32
	warnings []string
}

// listApps is the one implementation behind ListApps (RPC) and HttpListApps
// and HttpGetApp (REST).
//
// A caller could add up the sandbox listing itself, but only by knowing that an
// app's database is a separate sandbox with a different label spelling and a
// different kind. Doing it here means "what is app X using" has one answer that
// does not depend on the caller reconstructing Miren's model.
// The directory is returned alongside the listing because the app detail path
// needs the same entity view -- to say which contributing sandboxes still exist
// -- and loading it twice would mean two passes over the entity store per call.
func (s *Server) listApps(ctx context.Context, f filter, w window, ord ordering) (*appListing, *directory, error) {
	// The directory is still loaded, but for a narrower job than it used to do.
	// It no longer supplies the figures -- those come from the store, grouped
	// by app -- only each app's entity id, and a row for an app that exists but
	// has gone quiet. Every kind is loaded regardless of the addon setting:
	// addons are needed either to be counted or to be reported as the zero they
	// were excluded at.
	dir, err := s.loadDirectory(ctx, filter{node: f.node, includeSystem: true})
	if err != nil {
		return nil, nil, err
	}

	m, warnings := s.appSamples(ctx, f.node, w, dir)

	rows, cluster := buildAppRows(dir, m, f.app, f.includeAddons)

	out := &appListing{cluster: cluster, warnings: warnings, total: int32(len(rows))}

	sortApps(rows, ord)
	out.rows = rows[:ord.truncate(len(rows))]

	return out, dir, nil
}

// ListApps is the RPC surface.
func (s *Server) ListApps(ctx context.Context, state *usage_v1alpha.ResourceUsageListApps) error {
	args := state.Args()
	w := windowFrom(args.Window())

	listing, _, err := s.listApps(ctx, selectorToFilter(args.Selector()), w, orderingFrom(args.Ordering()))
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetApps(listing.rows)
	res.SetCluster(listing.cluster.encode())
	res.SetTotalCount(listing.total)
	res.SetWindow(w.encode())
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// HttpListApps serves GET /api/v1/usage/apps.
func (s *Server) HttpListApps(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpListApps) error {
	args := state.Args()

	w := restWindow(args.Since(), args.Until(), args.Aggregate())
	f := filter{node: args.Node(), includeAddons: !args.HasAddons() || args.Addons()}
	ord := ordering{sort: args.Sort(), order: args.Order(), limit: int(args.Limit())}

	listing, _, err := s.listApps(ctx, f, w, ord)
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetApps(listing.rows)
	res.SetCluster(listing.cluster.encode())
	res.SetTotalCount(listing.total)
	res.SetWindow(w.encode())
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// appDetailOptions is what a caller wants alongside one app's usage.
type appDetailOptions struct {
	series            bool
	includeAddons     bool
	step              time.Duration
	contributors      bool
	contributorSeries bool
}

// appDetailResult is what the core produces, before either surface shapes it
// into its own generated result type. Those types are write-only, so the core
// cannot hand one to the other -- it returns plain values instead.
type appDetailResult struct {
	usage        *usage_v1alpha.AppUsage
	series       []*usage_v1alpha.UsageSeries
	contributors []*usage_v1alpha.AppContributor
	warnings     []string

	// step is the resolution the series were rendered at, zero when none were
	// asked for.
	step time.Duration
}

// appDetailSetter is the shape both surfaces' result types share.
type appDetailSetter interface {
	SetUsage(*usage_v1alpha.AppUsage)
	SetSeries([]*usage_v1alpha.UsageSeries)
	SetContributors([]*usage_v1alpha.AppContributor)
	SetWindow(*usage_v1alpha.UsageWindow)
	SetWarnings([]string)
}

func (d *appDetailResult) apply(set appDetailSetter, w window) {
	set.SetUsage(d.usage)
	set.SetSeries(d.series)
	set.SetContributors(d.contributors)

	// The window reports the step the series actually used, not the one asked
	// for: an unset or too-fine request is resolved server-side.
	w.step = d.step
	set.SetWindow(w.encode())
	set.SetWarnings(d.warnings)
}

// appDetail is the one implementation behind GetApp (RPC) and HttpGetApp
// (REST).
//
// The row is what listApps would have produced for this one app; the series is
// what a row cannot say. A row collapses the window to one number, and "what
// has this app been doing" needs the shape of it.
func (s *Server) appDetail(
	ctx context.Context,
	name string,
	w window,
	opts appDetailOptions,
) (*appDetailResult, error) {
	if name == "" {
		return nil, fmt.Errorf("an app name is required")
	}

	// Asking for contributor histories is asking for contributors. Requiring
	// both flags would only be a way to get the combination wrong and receive
	// an empty list.
	if opts.contributorSeries {
		opts.contributors = true
	}

	listing, dir, err := s.listApps(ctx, filter{app: name, includeAddons: opts.includeAddons}, w, ordering{})
	if err != nil {
		return nil, err
	}

	if len(listing.rows) == 0 {
		// Not "has no running sandboxes" any more. Rows are sourced from the
		// metrics store, so an app deleted this morning still answers for a
		// window that contains it; an empty result means the window genuinely
		// holds nothing.
		return nil, fmt.Errorf("app %q has no usage in this window", name)
	}

	out := &appDetailResult{usage: listing.rows[0], warnings: listing.warnings}

	sel := labelSelector(map[string]string{labelApp: name})

	if opts.series || opts.contributorSeries {
		out.step = resolveStep(w, opts.step)
	}

	if opts.series {
		series, warnings := s.appSeries(ctx, sel, w, out.step, opts.includeAddons)
		out.series = series
		out.warnings = append(out.warnings, warnings...)
	}

	if opts.contributors {
		// The step the contributor histories use is the one already resolved
		// above, so a stacked chart lines up with the app series rather than
		// being sampled at its own resolution.
		withStep := opts
		withStep.step = out.step

		contributors, warnings := s.appContributors(ctx, sel, w, withStep, dir)
		out.contributors = contributors
		out.warnings = append(out.warnings, warnings...)
	}

	return out, nil
}

// GetApp is the RPC surface.
func (s *Server) GetApp(ctx context.Context, state *usage_v1alpha.ResourceUsageGetApp) error {
	args := state.Args()

	// Addons count toward an app's figures unless the caller says otherwise, so
	// the absent case has to be told apart from an explicit false. The
	// generated getter cannot do that on its own.
	opts := appDetailOptions{includeAddons: true}
	if o := args.Options(); o != nil {
		opts.series = o.IncludeSeries()
		opts.includeAddons = !o.HasIncludeAddons() || o.IncludeAddons()
		opts.step = time.Duration(o.StepSeconds()) * time.Second
		opts.contributors = o.IncludeContributors()
		opts.contributorSeries = o.ContributorSeries()
	}

	w := windowFrom(args.Window())

	detail, err := s.appDetail(ctx, args.App(), w, opts)
	if err != nil {
		return err
	}

	detail.apply(state.Results(), w)

	return nil
}

// HttpGetApp serves GET /api/v1/usage/apps/{app}, addressing one app.
func (s *Server) HttpGetApp(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpGetApp) error {
	args := state.Args()

	opts := appDetailOptions{
		series:            args.Series(),
		includeAddons:     !args.HasAddons() || args.Addons(),
		contributors:      args.Contributors(),
		contributorSeries: args.ContributorSeries(),
	}
	if d, ok := parseRESTDuration(args.Step()); ok {
		opts.step = d
	}

	w := restWindow(args.Since(), args.Until(), args.Aggregate())

	detail, err := s.appDetail(ctx, args.App(), w, opts)
	if err != nil {
		return err
	}

	detail.apply(state.Results(), w)

	return nil
}

// appKey identifies one slice of an app's usage as the metrics store holds it:
// the app, and whether the sandboxes behind it are the app's own services or
// the addons it owns.
type appKey struct {
	app  string
	kind string
}

// appMetrics is what the store says about every app in the window.
type appMetrics struct {
	cpu    map[appKey]float64
	memory map[appKey]float64

	// counts is how many sandboxes reported, which is not the same as how many
	// were scheduled. A sandbox that never started never reports and so is
	// never counted.
	counts map[appKey]int64
}

func newAppMetrics() appMetrics {
	return appMetrics{
		cpu:    map[appKey]float64{},
		memory: map[appKey]float64{},
		counts: map[appKey]int64{},
	}
}

// keys returns every group the store returned, in a fixed order.
//
// The order matters because it decides where an app the entity store has
// already forgotten lands in the listing, and a row that moved between two
// identical calls would read as a change in the cluster.
func (m appMetrics) keys() []appKey {
	seen := map[appKey]bool{}
	var keys []appKey

	add := func(k appKey) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}

	for k := range m.cpu {
		add(k)
	}
	for k := range m.memory {
		add(k)
	}
	for k := range m.counts {
		add(k)
	}

	slices.SortFunc(keys, func(a, b appKey) int {
		if c := cmp.Compare(a.app, b.app); c != 0 {
			return c
		}
		return cmp.Compare(a.kind, b.kind)
	})

	return keys
}

// appKeyOf reads a group out of a query result's labels. A row with no app
// label belongs to no app -- a shared addon server, or an unclassifiable
// sandbox -- and has nothing to roll up into.
func appKeyOf(labels map[string]string) (appKey, bool) {
	app := labels[labelApp]
	if app == "" {
		return appKey{}, false
	}
	return appKey{app: app, kind: labels[labelKind]}, true
}

// appTally accumulates one app's groups before they become a row.
type appTally struct {
	app      string
	appID    string
	services totals
	addons   totals

	serviceCount int64
	addonCount   int64

	// sampled records whether this app reported anything. An app that is
	// entirely silent is reported as stale rather than as idle.
	sampled bool

	// live records whether the app still has sandboxes in the entity store. An
	// app that does not is reported as historical: it is in the listing only
	// because the window reaches back far enough to contain its samples.
	live bool
}

// buildAppRows unions what the metrics store measured with what the entity
// store still knows about.
//
// Neither source alone is the row set. The store holds the numbers, and holds
// them for a month, so it is the only source that can answer a window longer
// than the current deployment -- an app redeployed on Tuesday has no live
// sandbox that can account for Monday. But an app whose sandboxes have all
// stopped reporting has no samples at all, and dropping it would make a broken
// telemetry pipeline look like an idle cluster.
//
// So: the entity pass contributes the apps that exist and their entity ids, the
// metric pass contributes the figures and any app the entity store has already
// swept, and a row says which of the two it came from through stale and
// historical.
func buildAppRows(
	dir *directory,
	m appMetrics,
	appFilter string,
	includeAddons bool,
) ([]*usage_v1alpha.AppUsage, totals) {
	tallies := map[string]*appTally{}
	order := []string{}

	tally := func(app string) *appTally {
		t, ok := tallies[app]
		if !ok {
			t = &appTally{app: app}
			tallies[app] = t
			order = append(order, app)
		}
		return t
	}

	// Entities first, so a live app keeps the position it has always had and
	// only the historical ones are appended after.
	for _, sb := range dir.sandboxes {
		app := sb.ref.App()
		if app == "" {
			// A sandbox belonging to no app -- a shared addon server, or an
			// unclassifiable one -- has nothing to roll up into.
			continue
		}
		if appFilter != "" && app != appFilter {
			continue
		}
		if sb.ref.Kind() == string(compute.KindAddon) && !includeAddons {
			continue
		}

		t := tally(app)
		t.live = true
		if t.appID == "" {
			t.appID = sb.ref.AppId()
		}
	}

	for _, k := range m.keys() {
		if appFilter != "" && k.app != appFilter {
			continue
		}

		isAddon := k.kind == string(compute.KindAddon)
		if isAddon && !includeAddons {
			continue
		}

		t := tally(k.app)

		cores, haveCPU := m.cpu[k]
		bytes, haveMem := m.memory[k]
		if haveCPU || haveMem {
			t.sampled = true
		}

		if isAddon {
			t.addons.cpuCores += cores
			t.addons.memoryBytes += int64(bytes)
			t.addonCount += m.counts[k]
		} else {
			t.services.cpuCores += cores
			t.services.memoryBytes += int64(bytes)
			t.serviceCount += m.counts[k]
		}
	}

	rows := make([]*usage_v1alpha.AppUsage, 0, len(order))
	var cluster totals

	for _, name := range order {
		t := tallies[name]

		total := totals{
			cpuCores:    t.services.cpuCores + t.addons.cpuCores,
			memoryBytes: t.services.memoryBytes + t.addons.memoryBytes,
		}

		var row usage_v1alpha.AppUsage
		row.SetApp(t.app)
		row.SetAppId(t.appID)
		row.SetTotal(total.encode())
		row.SetServices(t.services.encode())
		row.SetAddons(t.addons.encode())
		row.SetSandboxCount(t.serviceCount + t.addonCount)
		row.SetServiceCount(t.serviceCount)
		row.SetAddonCount(t.addonCount)
		row.SetStale(!t.sampled)
		row.SetHistorical(!t.live)

		cluster.cpuCores += total.cpuCores
		cluster.memoryBytes += total.memoryBytes

		rows = append(rows, &row)
	}

	return rows, cluster
}

// appSamples reads one row per app and kind straight from the store.
//
// Grouping by app in the query, rather than by sandbox with the rollup done
// afterwards, is what lets a historical window answer at all: a sandbox that
// died an hour ago is gone from the entity store within the hour, but its
// samples are kept for a month, and only a query that never mentions a sandbox
// id can reach them.
//
// The kind rides along in the grouping so the services-versus-addons split
// survives it. That split used to be made per sandbox because the metric labels
// were thought not to carry it; miren.kind is now stamped onto every series
// from the same compute.SandboxKind the entity path uses, so the store can make
// the same distinction the entity model does.
//
// Three queries answer the whole cluster, and none of them grows with the
// number of sandboxes.
func (s *Server) appSamples(
	ctx context.Context,
	node string,
	w window,
	dir *directory,
) (appMetrics, []string) {
	m := newAppMetrics()

	if s.Reader == nil {
		return m, []string{"no metrics backend configured; usage figures are unavailable"}
	}

	selector := ""
	if node != "" {
		if n := matchNode(dir.nodes, node); n != nil {
			selector = labelSelector(map[string]string{labelNode: string(n.id)})
		}
	}

	dur := w.duration()
	group := groupKey(labelApp, labelKind)

	var warnings []string

	collect := func(what, query string, store func(appKey, float64)) {
		rows, err := s.instantRows(ctx, query, w.end)
		if err != nil {
			warnings = append(warnings, what+" unavailable: "+err.Error())
			return
		}
		for _, r := range rows {
			if k, ok := appKeyOf(r.labels); ok {
				store(k, r.value)
			}
		}
	}

	collect("cpu usage", cpuCoresQuery(group, selector, dur, w.aggregate),
		func(k appKey, v float64) { m.cpu[k] = v })

	collect("memory usage", memoryBytesQuery(group, selector, dur, w.aggregate),
		func(k appKey, v float64) { m.memory[k] = v })

	// The counts say what a row is made of, so "1.5 cores of addon" can be
	// attributed to something. Their absence degrades a column rather than the
	// answer, which is why this is a warning and not a failure.
	collect("sandbox counts", sandboxCountQuery(selector, dur),
		func(k appKey, v float64) { m.counts[k] = int64(v) })

	return m, warnings
}

// appSeries fetches an app's CPU and memory over time, summed across every
// sandbox that belongs to it.
//
// It groups by kind as well as by app and sums here rather than in the query,
// because a caller may have excluded addons and a query that had already added
// them in could not be taken apart again. The selector pins one app, so at most
// two series come back per metric.
//
// That summing is what sandboxSeries does not do: it flattens every returned
// series into one list of points, which is only safe because its selector pins
// a single sandbox. Flattening here would concatenate an app's services and the
// database behind it into one undifferentiated run.
func (s *Server) appSeries(
	ctx context.Context,
	selector string,
	w window,
	step time.Duration,
	includeAddons bool,
) ([]*usage_v1alpha.UsageSeries, []string) {
	if s.Reader == nil {
		return nil, nil
	}

	group := groupKey(labelApp, labelKind)

	queries := []struct {
		metric string
		query  string
	}{
		{"cpu_cores", cpuCoresQuery(group, selector, step, aggregateAvg)},
		{"memory_bytes", memoryBytesQuery(group, selector, step, aggregateLast)},
	}

	var out []*usage_v1alpha.UsageSeries
	var warnings []string

	for _, q := range queries {
		result, err := s.Reader.RangeQuery(ctx, q.query, w.start, w.end, promDuration(step))
		if err != nil {
			warnings = append(warnings, q.metric+" history unavailable: "+err.Error())
			continue
		}

		// Every returned series is evaluated at the same step, so adding by
		// timestamp lines the kinds up without any interpolation.
		byTime := map[int64]float64{}
		for _, r := range result.Data.Result {
			if !includeAddons && r.Metric[labelKind] == string(compute.KindAddon) {
				continue
			}
			for _, v := range r.Values {
				at, val, ok := parseSample(v)
				if !ok {
					continue
				}
				byTime[at] += val
			}
		}

		var series usage_v1alpha.UsageSeries
		series.SetMetric(q.metric)
		series.SetPoints(pointsInOrder(byTime))
		out = append(out, &series)
	}

	return out, warnings
}

func sortApps(rows []*usage_v1alpha.AppUsage, ord ordering) {
	var (
		less      func(a, b *usage_v1alpha.AppUsage) bool
		ascending bool
	)

	switch strings.ToLower(strings.TrimSpace(ord.sort)) {
	case "memory", "mem":
		less = func(a, b *usage_v1alpha.AppUsage) bool { return a.Total().MemoryBytes() < b.Total().MemoryBytes() }
	case "sandboxes":
		less = func(a, b *usage_v1alpha.AppUsage) bool { return a.SandboxCount() < b.SandboxCount() }
	case "name", "app":
		less, ascending = func(a, b *usage_v1alpha.AppUsage) bool { return a.App() < b.App() }, true
	default:
		less = func(a, b *usage_v1alpha.AppUsage) bool { return a.Total().CpuCores() < b.Total().CpuCores() }
	}

	desc := ord.direction(ascending)

	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}
