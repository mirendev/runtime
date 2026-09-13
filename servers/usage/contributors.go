package usage

import (
	"context"
	"sort"
	"time"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

// This file answers "which sandboxes made up that number".
//
// An app-level figure cannot say it, and over any window longer than one
// deployment the answer is mostly sandboxes that no longer exist -- a dead one
// is swept from the entity store within the hour, while its samples are kept
// for a month. So the breakdown is read from the metric labels, which carry the
// service, version, node and kind alongside the sandbox id and outlive the
// entity by two orders of magnitude.

// contributorIdentity is what the labels say a sandbox was. It is the map key
// as well as the row, because the aggregate queries group by all of it at once
// and the two have to line up.
type contributorIdentity struct {
	sandbox string
	service string
	version string
	node    string
	kind    string
}

func identityOf(labels map[string]string) (contributorIdentity, bool) {
	sandbox := labels[labelSandbox]
	if sandbox == "" {
		return contributorIdentity{}, false
	}

	return contributorIdentity{
		sandbox: sandbox,
		service: labels[labelService],
		version: labels[labelVersion],
		node:    labels[labelNode],
		kind:    labels[labelKind],
	}, true
}

// contributorTally accumulates one sandbox before it becomes a row.
type contributorTally struct {
	id contributorIdentity

	// cpuSeconds is how much CPU time this sandbox consumed in the window.
	// Cores is derived from it and the active span rather than queried, because
	// a rate averaged across a window the sandbox did not live through is not a
	// figure anyone wants.
	cpuSeconds float64
	bytes      float64

	first  time.Time
	last   time.Time
	series []*usage_v1alpha.UsageSeries
}

// activeSeconds is how long this sandbox was reporting inside the window.
//
// Floored at one second so a sandbox seen exactly once still divides. The
// collectors sample about once a second, so a span shorter than that means one
// sample, not an instantaneous sandbox.
func (t *contributorTally) activeSeconds() float64 {
	if t.first.IsZero() || t.last.IsZero() {
		return 0
	}

	span := t.last.Sub(t.first).Seconds()
	if span < 1 {
		return 1
	}

	return span
}

// cores is the average cores this sandbox used while it was running.
func (t *contributorTally) cores() float64 {
	active := t.activeSeconds()
	if active <= 0 {
		return 0
	}

	return t.cpuSeconds / active
}

// contributorGrouping is the identity the aggregate queries group by. Every
// label here is one the sandbox controller stamps on every series, so grouping
// by them costs no extra cardinality: they are functionally dependent on the
// sandbox id.
func contributorGrouping() string {
	return groupKey(labelSandbox, labelService, labelVersion, labelNode, labelKind)
}

// appContributors breaks an app's usage down by the sandboxes that produced it.
//
// Four instant queries regardless of how many sandboxes come back, plus two
// range queries when histories are asked for. None of them grows with the
// number of sandboxes, which is what makes a week-long question affordable.
func (s *Server) appContributors(
	ctx context.Context,
	selector string,
	w window,
	opts appDetailOptions,
	dir *directory,
) ([]*usage_v1alpha.AppContributor, []string) {
	if s.Reader == nil {
		return nil, nil
	}

	dur := w.duration()
	group := contributorGrouping()

	tallies := map[string]*contributorTally{}
	var warnings []string

	tally := func(id contributorIdentity) *contributorTally {
		t, ok := tallies[id.sandbox]
		if !ok {
			t = &contributorTally{id: id}
			tallies[id.sandbox] = t
		}
		return t
	}

	collect := func(what, query string, store func(*contributorTally, float64)) {
		rows, err := s.instantRows(ctx, query, w.end)
		if err != nil {
			warnings = append(warnings, what+" unavailable: "+err.Error())
			return
		}
		for _, r := range rows {
			id, ok := identityOf(r.labels)
			if !ok {
				continue
			}

			// A caller that excluded addons from the row excluded them from
			// the breakdown too. Leaving them in would list sandboxes the row
			// above does not count, and their cpu_seconds would no longer sum
			// to it -- which is the one property that field promises.
			if !opts.includeAddons && id.kind == string(compute.KindAddon) {
				continue
			}

			store(tally(id), r.value)
		}
	}

	collect("contributor cpu", contributorCPUSecondsQuery(group, selector, dur),
		func(t *contributorTally, v float64) { t.cpuSeconds = v })

	collect("contributor memory", contributorMemoryQuery(group, selector, dur, w.aggregate),
		func(t *contributorTally, v float64) { t.bytes = v })

	// The seen queries group by sandbox alone, so their rows carry no service,
	// version or kind. They only ever update a tally the aggregates already
	// created: a row built from a sample time alone would name nothing and
	// report nothing, which is an id rather than a contributor. That matters
	// when the aggregates fail and these succeed, which is the one case where
	// the two query sets disagree about who exists.
	updateSeen := func(what, query string, store func(*contributorTally, float64)) {
		rows, err := s.instantRows(ctx, query, w.end)
		if err != nil {
			warnings = append(warnings, what+" unavailable: "+err.Error())
			return
		}
		for _, r := range rows {
			if t := tallies[r.labels[labelSandbox]]; t != nil {
				store(t, r.value)
			}
		}
	}

	updateSeen("contributor start times", firstSeenQuery(selector, dur),
		func(t *contributorTally, v float64) { t.first = time.Unix(int64(v), 0) })

	updateSeen("contributor end times", lastSeenQuery(selector, dur),
		func(t *contributorTally, v float64) { t.last = time.Unix(int64(v), 0) })

	if opts.contributorSeries {
		seriesWarnings := s.attachContributorSeries(ctx, selector, w, opts.step, tallies)
		warnings = append(warnings, seriesWarnings...)
	}

	return buildContributorRows(tallies, dir), warnings
}

// attachContributorSeries fetches every contributor's history in two range
// queries -- one per metric -- rather than two per sandbox, by grouping on the
// sandbox label and splitting the result afterwards.
func (s *Server) attachContributorSeries(
	ctx context.Context,
	selector string,
	w window,
	step time.Duration,
	tallies map[string]*contributorTally,
) []string {
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

		for _, r := range result.Data.Result {
			t := tallies[r.Metric[labelSandbox]]
			if t == nil {
				// A sandbox with a history but no aggregate has no row to hang
				// it on. Inventing one here would contradict the totals.
				continue
			}

			byTime := map[int64]float64{}
			for _, v := range r.Values {
				at, val, ok := parseSample(v)
				if !ok {
					continue
				}
				byTime[at] = val
			}

			var series usage_v1alpha.UsageSeries
			series.SetMetric(q.metric)
			series.SetPoints(pointsInOrder(byTime))
			t.series = append(t.series, &series)
		}
	}

	return warnings
}

// buildContributorRows turns the tallies into rows, busiest first.
//
// The directory is consulted only for what the labels cannot say: whether the
// sandbox still exists, and the short ids someone could type. A short id is
// allocated and stored on the entity rather than derived from its id
// (pkg/entity/shortid.go), so once the entity is swept there is nothing left to
// recover it from -- those fields stay empty and alive reports why.
func buildContributorRows(
	tallies map[string]*contributorTally,
	dir *directory,
) []*usage_v1alpha.AppContributor {
	live := map[string]*usage_v1alpha.SandboxRef{}
	versions := map[string]*appInfo{}
	if dir != nil {
		for _, sb := range dir.sandboxes {
			live[sb.ref.Sandbox()] = sb.ref
		}
		versions = dir.versions
	}

	rows := make([]*usage_v1alpha.AppContributor, 0, len(tallies))

	for _, t := range tallies {
		var c usage_v1alpha.AppContributor
		c.SetSandbox(t.id.sandbox)
		c.SetService(t.id.service)
		c.SetVersion(t.id.version)
		c.SetNode(t.id.node)
		c.SetKind(t.id.kind)

		var cpu usage_v1alpha.CpuUsage
		cpu.SetCores(t.cores())
		c.SetCpu(&cpu)
		c.SetCpuSeconds(t.cpuSeconds)

		var mem usage_v1alpha.MemoryUsage
		mem.SetBytes(int64(t.bytes))
		c.SetMemory(&mem)

		if !t.first.IsZero() {
			c.SetFirstSeen(standard.ToTimestamp(t.first))
		}
		if !t.last.IsZero() {
			c.SetLastSeen(standard.ToTimestamp(t.last))
		}

		if ref := live[t.id.sandbox]; ref != nil {
			c.SetAlive(true)
			c.SetSandboxShortId(ref.SandboxShortId())
		}

		// The version short id comes from the version rather than from the
		// sandbox, so a replaced deployment still names one. App versions are
		// not swept when they stop running, which makes this the one identifier
		// a gone contributor can still offer.
		if v := versions[t.id.version]; v != nil {
			c.SetVersionShortId(v.shortID)
		}

		c.SetSeries(t.series)

		rows = append(rows, &c)
	}

	sortContributors(rows)

	return rows
}

// sortContributors puts the busiest first, so the sandbox that explains a spike
// is the row someone reads rather than one they scroll to. Ties break on the
// sandbox id so two identical calls agree.
func sortContributors(rows []*usage_v1alpha.AppContributor) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		// Ordered by CPU time consumed rather than by cores, because that is
		// the figure that compares across sandboxes with different lifetimes:
		// a ten-minute sandbox at two cores contributed far less than an
		// all-week one at half a core.
		if a.CpuSeconds() != b.CpuSeconds() {
			return a.CpuSeconds() > b.CpuSeconds()
		}
		if a.Memory().Bytes() != b.Memory().Bytes() {
			return a.Memory().Bytes() > b.Memory().Bytes()
		}
		return a.Sandbox() < b.Sandbox()
	})
}
