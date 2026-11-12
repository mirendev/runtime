//go:build linux

package sandbox

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/metrics"
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

// A cgroup without memory.swap.current (swap accounting off) reads as 0 swap
// from Stat. The runner must leave its swap series out rather than store that
// 0, or the usage API would report "no swap" for a sandbox nobody measured.
// The root cgroup never has the file, so it stands in for an unaccounted one.
func TestMetrics_SwapSeriesOnlyWhenAccounted(t *testing.T) {
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
		t.Skip("needs a cgroup2 hierarchy at " + cgroupRoot)
	}
	self, err := os.ReadFile("/proc/self/cgroup")
	require.NoError(t, err)
	selfGroup := strings.TrimSpace(strings.TrimPrefix(string(self), "0::"))
	if _, err := os.Stat(filepath.Join(cgroupRoot, selfGroup, "memory.swap.current")); err != nil {
		t.Skip("this process's cgroup has no swap accounting to compare against")
	}

	var (
		mu      sync.Mutex
		written strings.Builder
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		written.Write(body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	log := slog.New(slog.DiscardHandler)
	writer := metrics.NewVictoriaMetricsWriter(log, strings.TrimPrefix(srv.URL, "http://"), 5*time.Second)
	cpu := metrics.NewCPUUsage(log, writer, nil)
	require.NoError(t, cpu.Setup())
	mem := metrics.NewMemoryUsage(log, writer, nil)
	require.NoError(t, mem.Setup())

	m := NewMetrics()
	m.Log, m.CPUUsage, m.MemUsage = log, cpu, mem
	require.NoError(t, m.Add("sb-accounted", map[string]string{"": selfGroup}, map[string]string{"miren.sandbox": "sb-accounted"}))
	require.NoError(t, m.Add("sb-unaccounted", map[string]string{"": "/"}, map[string]string{"miren.sandbox": "sb-unaccounted"}))

	// The first pass only primes the CPU baseline; the second records.
	for range 2 {
		require.NoError(t, m.writeStatsToStorage(context.Background()))
	}
	writer.Flush()

	mu.Lock()
	var swapLines []string
	for _, line := range strings.Split(written.String(), "\n") {
		if strings.HasPrefix(line, "swap_usage_bytes{") {
			swapLines = append(swapLines, line)
		}
	}
	mu.Unlock()

	require.Len(t, swapLines, 1)
	assert.Contains(t, swapLines[0], `miren_sandbox="sb-accounted"`)
}
