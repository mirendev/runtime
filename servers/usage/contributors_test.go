package usage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/rpc/standard"
)

// tallyOf builds a contributor that reported cpuSeconds of CPU time and bytes
// of memory across a one-minute active span, so cores works out to cpuSeconds
// per minute.
func tallyOf(sandbox, service, version string, cpuSeconds, bytes float64) *contributorTally {
	start := time.Unix(1_700_000_000, 0)

	return &contributorTally{
		id: contributorIdentity{
			sandbox: sandbox,
			service: service,
			version: version,
			node:    "node/miren",
			kind:    string(compute.KindApp),
		},
		cpuSeconds: cpuSeconds,
		bytes:      bytes,
		first:      start,
		last:       start.Add(time.Minute),
	}
}

func findContributor(rows []*usage_v1alpha.AppContributor, sandbox string) *usage_v1alpha.AppContributor {
	for _, r := range rows {
		if r.Sandbox() == sandbox {
			return r
		}
	}
	return nil
}

// Everything that identifies a contributor is read off the metric labels, which
// is the whole point: the sandbox entity that used to hold this is swept within
// the hour, and the samples are kept for a month.
func TestContributorIdentityComesFromLabels(t *testing.T) {
	id, ok := identityOf(map[string]string{
		labelSandbox: "sandbox/shop-web-abc",
		labelService: "web",
		labelVersion: "app_version/shop-v1",
		labelNode:    "node/miren",
		labelKind:    string(compute.KindApp),
	})

	require.True(t, ok)
	assert.Equal(t, "web", id.service)
	assert.Equal(t, "app_version/shop-v1", id.version)
	assert.Equal(t, "node/miren", id.node)

	// A series with no sandbox label identifies no contributor.
	_, ok = identityOf(map[string]string{labelApp: "shop"})
	assert.False(t, ok)
}

// The reason this exists: a week's worth of contributors is mostly sandboxes
// the entity store has already forgotten, and they still have to account for
// themselves.
func TestContributorRowsIncludeSweptSandboxes(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{row("sandbox/live", "shop", string(compute.KindApp))}}

	tallies := map[string]*contributorTally{
		"sandbox/live": tallyOf("sandbox/live", "web", "app_version/v2", 0.5, 100),
		"sandbox/gone": tallyOf("sandbox/gone", "web", "app_version/v1", 2.0, 900),
	}

	rows := buildContributorRows(tallies, dir)
	require.Len(t, rows, 2)

	gone := findContributor(rows, "sandbox/gone")
	require.NotNil(t, gone)
	assert.False(t, gone.Alive())
	assert.InDelta(t, 2.0, gone.CpuSeconds(), 0.001)
	assert.Equal(t, "web", gone.Service(), "the labels still say what it was")
	assert.Equal(t, "app_version/v1", gone.Version())

	// A short id is allocated onto the entity, not derived from its id, so a
	// swept sandbox has nothing left to recover one from. Reporting a guess
	// would hand someone an identifier that resolves to nothing.
	assert.Equal(t, "", gone.SandboxShortId())

	assert.True(t, findContributor(rows, "sandbox/live").Alive())
}

// The busiest row is the one someone is looking for, so it must not be the one
// they have to scroll to.
func TestContributorRowsAreBusiestFirst(t *testing.T) {
	tallies := map[string]*contributorTally{
		"sandbox/quiet": tallyOf("sandbox/quiet", "web", "v1", 0.1, 100),
		"sandbox/busy":  tallyOf("sandbox/busy", "web", "v1", 3.0, 100),
		"sandbox/mid":   tallyOf("sandbox/mid", "worker", "v1", 1.0, 100),
	}

	rows := buildContributorRows(tallies, nil)

	order := make([]string, 0, len(rows))
	for _, r := range rows {
		order = append(order, r.Sandbox())
	}
	assert.Equal(t, []string{"sandbox/busy", "sandbox/mid", "sandbox/quiet"}, order)
}

// Two sandboxes at the same CPU must not swap places between identical calls;
// a row that moved would read as a change in the cluster.
func TestContributorOrderIsStableOnTies(t *testing.T) {
	tallies := map[string]*contributorTally{
		"sandbox/b": tallyOf("sandbox/b", "web", "v1", 1.0, 500),
		"sandbox/a": tallyOf("sandbox/a", "web", "v1", 1.0, 500),
	}

	first := buildContributorRows(tallies, nil)
	second := buildContributorRows(tallies, nil)

	require.Len(t, first, 2)
	assert.Equal(t, "sandbox/a", first[0].Sandbox())
	assert.Equal(t, first[0].Sandbox(), second[0].Sandbox())
}

// Memory breaks a CPU tie, so two idle sandboxes still sort by what they held.
func TestContributorMemoryBreaksACpuTie(t *testing.T) {
	tallies := map[string]*contributorTally{
		"sandbox/small": tallyOf("sandbox/small", "web", "v1", 0, 100),
		"sandbox/large": tallyOf("sandbox/large", "web", "v1", 0, 900),
	}

	rows := buildContributorRows(tallies, nil)
	assert.Equal(t, "sandbox/large", rows[0].Sandbox())
}

// first_seen and last_seen are what turn a week of contributors into a sequence
// of deployments rather than an undated pile.
func TestContributorRowsCarryTheirWindow(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	end := start.Add(90 * time.Minute)

	tally := tallyOf("sandbox/a", "web", "v1", 1, 100)
	tally.first = start
	tally.last = end

	rows := buildContributorRows(map[string]*contributorTally{"sandbox/a": tally}, nil)
	require.Len(t, rows, 1)

	assert.Equal(t, start.Unix(), standard.FromTimestamp(rows[0].FirstSeen()).Unix())
	assert.Equal(t, end.Unix(), standard.FromTimestamp(rows[0].LastSeen()).Unix())
}

// A sandbox whose sample times are unknown reports no window rather than the
// epoch, which would read as having run in 1970.
func TestContributorRowsOmitAnUnknownWindow(t *testing.T) {
	tally := tallyOf("sandbox/a", "web", "v1", 1, 100)
	tally.first = time.Time{}
	tally.last = time.Time{}

	rows := buildContributorRows(map[string]*contributorTally{"sandbox/a": tally}, nil)

	require.Len(t, rows, 1)
	assert.False(t, rows[0].HasFirstSeen())
	assert.False(t, rows[0].HasLastSeen())
	assert.InDelta(t, 0, rows[0].Cpu().Cores(), 0.001,
		"with no span there is nothing to divide by, and a guess would be worse than zero")
}

// Grouping by the identity labels costs no extra cardinality -- every one of
// them is functionally dependent on the sandbox id -- and it is what lets a
// contributor name its service and version without an entity lookup.
func TestContributorGroupingCarriesTheIdentityLabels(t *testing.T) {
	g := contributorGrouping()

	for _, label := range []string{labelSandbox, labelService, labelVersion, labelNode, labelKind} {
		assert.Contains(t, g, label)
	}
	assert.NotContains(t, g, "miren.", "a dotted label silently matches nothing")
}

// Each contributor's history is split out of one query per metric, not fetched
// per sandbox. Getting the split wrong would give every sandbox every other
// sandbox's points.
func TestContributorSeriesAreSplitBySandbox(t *testing.T) {
	stub := &rangeStub{series: []metrics.Result{
		{
			Metric: map[string]string{labelSandbox: "sandbox/a"},
			Values: [][]any{at(100, "1"), at(160, "2")},
		},
		{
			Metric: map[string]string{labelSandbox: "sandbox/b"},
			Values: [][]any{at(100, "9")},
		},
		{
			// A sandbox with a history but no aggregate has no row to hang it
			// on; inventing one would contradict the totals.
			Metric: map[string]string{labelSandbox: "sandbox/unknown"},
			Values: [][]any{at(100, "5")},
		},
	}}

	s := &Server{Reader: stub.reader(t)}
	w := window{start: time.Unix(100, 0), end: time.Unix(160, 0), aggregate: aggregateAvg}

	tallies := map[string]*contributorTally{
		"sandbox/a": tallyOf("sandbox/a", "web", "v1", 1, 100),
		"sandbox/b": tallyOf("sandbox/b", "web", "v1", 1, 100),
	}

	warnings := s.attachContributorSeries(context.Background(), "", w, time.Minute, tallies)
	require.Empty(t, warnings)

	// Two range queries answer every sandbox, not two per sandbox.
	require.Len(t, stub.queries, 2)
	for _, q := range stub.queries {
		assert.Contains(t, q, "by (miren_sandbox)")
	}

	a := tallies["sandbox/a"]
	require.Len(t, a.series, 2, "one series per metric")
	assert.Len(t, a.series[0].Points(), 2)

	b := tallies["sandbox/b"]
	require.Len(t, b.series, 2)
	require.Len(t, b.series[0].Points(), 1)
	assert.InDelta(t, 9.0, b.series[0].Points()[0].Value(), 0.001,
		"sandbox b must not inherit sandbox a's points")

	_, ok := tallies["sandbox/unknown"]
	assert.False(t, ok, "a history with no aggregate creates no contributor")
}

// A version is not swept when it stops running, so a contributor whose sandbox
// is long gone can still name a version someone can type. That makes it the one
// actionable identifier on a historical row.
func TestContributorVersionShortIdSurvivesTheSandbox(t *testing.T) {
	dir := &directory{
		versions: map[string]*appInfo{
			"app_version/shop-v1": {id: "app/shop", version: "v1", shortID: "e8v"},
		},
	}

	tallies := map[string]*contributorTally{
		"sandbox/gone": tallyOf("sandbox/gone", "web", "app_version/shop-v1", 1, 100),
	}

	rows := buildContributorRows(tallies, dir)
	require.Len(t, rows, 1)

	assert.False(t, rows[0].Alive())
	assert.Equal(t, "", rows[0].SandboxShortId(), "the sandbox entity is gone")
	assert.Equal(t, "e8v", rows[0].VersionShortId(), "the version entity is not")
}

// The reason contributors are measured against their own lifetime rather than
// the window: a sandbox that ran hot for ten minutes of a week did not use
// "almost nothing", it used two cores and then stopped.
func TestContributorCoresDescribeTheSandboxNotTheWindow(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)

	tally := tallyOf("sandbox/burst", "web", "v1", 0, 100)
	tally.first = start
	tally.last = start.Add(10 * time.Minute)
	tally.cpuSeconds = 1200 // two cores for ten minutes

	rows := buildContributorRows(map[string]*contributorTally{"sandbox/burst": tally}, nil)
	require.Len(t, rows, 1)

	assert.InDelta(t, 2.0, rows[0].Cpu().Cores(), 0.001,
		"two cores while running, not 1200s averaged across a week")
	assert.InDelta(t, 1200, rows[0].CpuSeconds(), 0.001,
		"seconds are the figure that still adds up")
}

// Ordering follows CPU time consumed, not the rate, because a rate cannot be
// compared between sandboxes that ran for different lengths of time.
func TestContributorOrderFollowsConsumptionNotRate(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)

	brief := tallyOf("sandbox/brief", "web", "v1", 0, 100)
	brief.first, brief.last = start, start.Add(time.Minute)
	brief.cpuSeconds = 120 // two cores, but only for a minute

	sustained := tallyOf("sandbox/sustained", "web", "v1", 0, 100)
	sustained.first, sustained.last = start, start.Add(10*time.Hour)
	sustained.cpuSeconds = 18000 // half a core all day

	rows := buildContributorRows(map[string]*contributorTally{
		"sandbox/brief":     brief,
		"sandbox/sustained": sustained,
	}, nil)

	assert.InDelta(t, 2.0, findContributor(rows, "sandbox/brief").Cpu().Cores(), 0.01)
	assert.InDelta(t, 0.5, findContributor(rows, "sandbox/sustained").Cpu().Cores(), 0.01)

	assert.Equal(t, "sandbox/sustained", rows[0].Sandbox(),
		"the one that actually consumed the app's CPU comes first")
}

// The seen queries group by sandbox alone, so a row of theirs names no service,
// version or kind. If the usage queries have failed and these have not, the two
// disagree about who exists, and building a contributor from a sample time
// alone would put a bare id in the breakdown with zeroes beside it.
func TestSeenTimesNeverInventAContributor(t *testing.T) {
	stub := &instantStub{
		// Usage fails; only the sample-time queries answer.
		fail: []string{metricCPUSeconds, "sum by (miren_sandbox, miren_service"},
		series: []metrics.Result{{
			Metric: map[string]string{labelSandbox: "sandbox/ghost"},
			Value:  at(1_700_000_000, "1700000000"),
		}},
	}

	s := &Server{Reader: stub.reader(t)}
	w := window{start: time.Unix(0, 0), end: time.Unix(3600, 0), aggregate: aggregateAvg}

	rows, warnings := s.appContributors(context.Background(), "", w, appDetailOptions{}, nil)

	assert.Empty(t, rows, "a sample time with no usage is an id, not a contributor")
	assert.NotEmpty(t, warnings, "the failed usage queries are still reported")
}
