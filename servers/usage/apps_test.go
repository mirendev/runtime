package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/usage/usage_v1alpha"
)

// row builds one directory entry. The ref is what the entity pass would have
// produced; the sample maps are keyed by the same sandbox id.
func row(sandbox, app, kind string) sandboxRow {
	var ref usage_v1alpha.SandboxRef
	ref.SetSandbox(sandbox)
	ref.SetApp(app)
	ref.SetAppId("app/" + app)
	ref.SetKind(kind)
	return sandboxRow{ref: &ref}
}

func findApp(rows []*usage_v1alpha.AppUsage, name string) *usage_v1alpha.AppUsage {
	for _, r := range rows {
		if r.App() == name {
			return r
		}
	}
	return nil
}

// The whole point of the rollup: an app's own services and the database it
// depends on are separate sandboxes, and a caller should not have to know that.
func TestAppRowsSumServicesAndAddons(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/web1", "shop", sandboxKindApp),
		row("sb/web2", "shop", sandboxKindApp),
		row("sb/pg", "shop", sandboxKindAddon),
	}}
	cpu := map[string]float64{"sb/web1": 0.5, "sb/web2": 0.25, "sb/pg": 1.5}
	mem := map[string]float64{"sb/web1": 100, "sb/web2": 100, "sb/pg": 800}

	rows, cluster := buildAppRows(dir, cpu, mem, "", true)
	require.Len(t, rows, 1)

	shop := rows[0]
	assert.InDelta(t, 2.25, shop.Total().CpuCores(), 0.001)
	assert.InDelta(t, 0.75, shop.Services().CpuCores(), 0.001)
	assert.InDelta(t, 1.5, shop.Addons().CpuCores(), 0.001)
	assert.Equal(t, int64(1000), shop.Total().MemoryBytes())

	// The counts say what the split is made of, so "1.5 cores of addon" can be
	// attributed to something.
	assert.Equal(t, int64(3), shop.SandboxCount())
	assert.Equal(t, int64(2), shop.ServiceCount())
	assert.Equal(t, int64(1), shop.AddonCount())

	assert.InDelta(t, 2.25, cluster.cpuCores, 0.001)
}

// Excluding addons answers "what is my code doing" rather than "what does this
// app cost". The addon block must then read zero rather than disappear, or the
// total and the split stop agreeing.
func TestAppRowsCanExcludeAddons(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/web", "shop", sandboxKindApp),
		row("sb/pg", "shop", sandboxKindAddon),
	}}
	cpu := map[string]float64{"sb/web": 0.5, "sb/pg": 1.5}

	rows, _ := buildAppRows(dir, cpu, nil, "", false)
	require.Len(t, rows, 1)

	assert.InDelta(t, 0.5, rows[0].Total().CpuCores(), 0.001)
	assert.InDelta(t, 0, rows[0].Addons().CpuCores(), 0.001)
	assert.Equal(t, int64(0), rows[0].AddonCount())
	assert.Equal(t, int64(1), rows[0].SandboxCount())
}

func TestAppRowsSeparateApps(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/a", "shop", sandboxKindApp),
		row("sb/b", "blog", sandboxKindApp),
		row("sb/c", "blog", sandboxKindAddon),
	}}
	cpu := map[string]float64{"sb/a": 1, "sb/b": 2, "sb/c": 3}

	rows, _ := buildAppRows(dir, cpu, nil, "", true)
	require.Len(t, rows, 2)

	assert.InDelta(t, 1.0, findApp(rows, "shop").Total().CpuCores(), 0.001)
	assert.InDelta(t, 5.0, findApp(rows, "blog").Total().CpuCores(), 0.001)
}

// An app whose sandboxes have all gone quiet is a finding. Dropping the row
// would make a broken telemetry pipeline look like an idle cluster.
func TestAppRowsReportSilentAppsAsStale(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/quiet", "shop", sandboxKindApp),
		row("sb/busy", "blog", sandboxKindApp),
	}}
	cpu := map[string]float64{"sb/busy": 1}

	rows, _ := buildAppRows(dir, cpu, nil, "", true)
	require.Len(t, rows, 2, "an app with no samples still gets a row")

	assert.True(t, findApp(rows, "shop").Stale())
	assert.False(t, findApp(rows, "blog").Stale())
}

// A sandbox belonging to no app -- a shared addon server -- has nothing to roll
// up into, and must not create a row named "".
func TestAppRowsSkipSandboxesWithNoApp(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/shared", "", sandboxKindAddon),
		row("sb/web", "shop", sandboxKindApp),
	}}
	cpu := map[string]float64{"sb/shared": 9, "sb/web": 1}

	rows, cluster := buildAppRows(dir, cpu, nil, "", true)

	require.Len(t, rows, 1)
	assert.Equal(t, "shop", rows[0].App())
	assert.InDelta(t, 1.0, cluster.cpuCores, 0.001,
		"an unattributable sandbox must not inflate the cluster total")
}

func TestAppRowsFilterByApp(t *testing.T) {
	dir := &directory{sandboxes: []sandboxRow{
		row("sb/a", "shop", sandboxKindApp),
		row("sb/b", "blog", sandboxKindApp),
	}}

	rows, _ := buildAppRows(dir, map[string]float64{"sb/a": 1, "sb/b": 2}, nil, "shop", true)

	require.Len(t, rows, 1)
	assert.Equal(t, "shop", rows[0].App())
}
