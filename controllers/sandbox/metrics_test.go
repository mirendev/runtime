//go:build linux

package sandbox

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetrics_AddIfAbsent_PreservesRegisteredEntry is the no-op contract the
// resume path depends on: a re-run of the saga's add-metrics action (or the
// reconciler's ensureMetrics before it routes to resume) must not discard the
// cgroup baseline an earlier registration established. Metrics.Add overwrites
// namedEntries[name] with a fresh *Cgroups and so loses the accumulated CPU
// delta (MIR-1013); AddIfAbsent returns early and leaves the entry alone. The
// saga fixes that path by routing addMetrics through AddIfAbsent.
func TestMetrics_AddIfAbsent_PreservesRegisteredEntry(t *testing.T) {
	m := NewMetrics()
	orig := &Cgroups{}
	m.namedEntries["app/web-sb-1"] = orig

	added, err := m.AddIfAbsent("app/web-sb-1",
		map[string]string{"": "/sys/fs/cgroup/nonexistent/path"}, nil)
	require.NoError(t, err)
	assert.False(t, added, "AddIfAbsent must report not-added for an already-monitored name")
	// The recorded entry must be the exact one already in the map, not a fresh
	// replacement Add would have installed after re-loading the cgroups.
	assert.Same(t, orig, m.namedEntries["app/web-sb-1"],
		"AddIfAbsent must not replace the recorded *Cgroups entry")

	// An unmonitored name still registers (Add and AddIfAbsent agree on the
	// first registration), so the happy-path saga action is unaffected.
	assert.True(t, m.Has("app/web-sb-1"))
	assert.False(t, m.Has("app/other-sb"))
}

// TestSandboxOps_AddMetrics_ResumesIdempotently pins the wiring the saga's
// add-metrics action relies on. ensureMetrics (case-same) registers a surviving
// sandbox via AddIfAbsent; on resume the create-sandbox saga re-runs
// addMetrics against the already-listening, already-monitored sandbox. The
// action calls sandboxOps.AddMetrics, which must therefore use AddIfAbsent -- a
// bare Add here would overwrite the entry (losing the CPU baseline) and, on
// linux, would also re-Load cgroup paths that may describe running containers.
func TestSandboxOps_AddMetrics_ResumesIdempotently(t *testing.T) {
	sbLogEntity := "app/web-sb-1"
	ctrl := &SandboxController{
		Metrics: NewMetrics(),
		Log:     slog.Default(),
	}
	orig := &Cgroups{}
	ctrl.Metrics.namedEntries[sbLogEntity] = orig

	ops := &sandboxOps{ctrl: ctrl}

	err := ops.AddMetrics(sbLogEntity,
		map[string]string{"": "/sys/fs/cgroup/nonexistent/path"}, nil)
	require.NoError(t, err,
		"AddMetrics must be a no-op for an already-monitored sandbox")
	assert.Same(t, orig, ctrl.Metrics.namedEntries[sbLogEntity],
		"sandboxOps.AddMetrics must not replace the recorded entry (AddIfAbsent wiring)")
}
