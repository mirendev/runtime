//go:build darwin

package sandbox

import (
	"context"
	"log/slog"
	"sync"

	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/metrics"
)

type Cgroups struct {
}

type Metrics struct {
	Log      *slog.Logger
	CPUUsage *metrics.CPUUsage
	MemUsage *metrics.MemoryUsage

	mu           sync.Mutex
	namedEntries map[string]*Cgroups
}

// NewMetrics creates a new Metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		namedEntries: make(map[string]*Cgroups),
	}
}

func (m *Metrics) Add(name string, pathes map[string]string, attributes map[string]string) error {
	return nil
}

func (m *Metrics) AddIfAbsent(name string, pathes map[string]string, attributes map[string]string) (bool, error) {
	return true, nil
}

func (m *Metrics) Has(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.namedEntries[name]
	return ok
}

func (m *Metrics) Remove(name string) error {
	return nil
}

func (m *Metrics) Gather(name string) ([]*metric_v1alpha.ContainerSnapshot, error) {
	return nil, nil
}

var _ metric_v1alpha.SandboxMetrics = (*Metrics)(nil)

func (m *Metrics) Snapshot(ctx context.Context, req *metric_v1alpha.SandboxMetricsSnapshot) error {
	return nil
}

func (m *Metrics) Monitor(ctx context.Context) {
}
