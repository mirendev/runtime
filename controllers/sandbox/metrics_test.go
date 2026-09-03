//go:build linux

package sandbox

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Add overwrites namedEntries[name] with a fresh *Cgroups, losing the
// accumulated CPU delta (MIR-1013). AddIfAbsent must leave the entry alone.
func TestMetrics_AddIfAbsent_PreservesRegisteredEntry(t *testing.T) {
	m := NewMetrics()
	orig := &Cgroups{}
	m.namedEntries["app/web-sb-1"] = orig

	added, err := m.AddIfAbsent("app/web-sb-1",
		map[string]string{"": "/sys/fs/cgroup/nonexistent/path"}, nil)
	require.NoError(t, err)
	assert.False(t, added, "AddIfAbsent must report not-added for an already-monitored name")
	assert.Same(t, orig, m.namedEntries["app/web-sb-1"],
		"AddIfAbsent must not replace the recorded *Cgroups entry")

	// A first registration still happens, so the happy path is unaffected.
	assert.True(t, m.Has("app/web-sb-1"))
	assert.False(t, m.Has("app/other-sb"))
}

// The saga's add-metrics action goes through sandboxOps.AddMetrics, so that is
// where the AddIfAbsent wiring has to hold.
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
