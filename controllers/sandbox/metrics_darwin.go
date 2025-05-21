//go:build darwin

package sandbox

import (
	"context"
	"log/slog"
	"sync"

	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/asm/autoreg"
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

var _ = autoreg.Register[Metrics]()

func (m *Metrics) Add(name string, pathes map[string]string, attributes map[string]string) error {
	return nil
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
