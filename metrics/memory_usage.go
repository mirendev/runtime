package metrics

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/units"
)

type MemoryUsage struct {
	Log    *slog.Logger
	Writer *VictoriaMetricsWriter `asm:"victoriametrics-writer,optional"`
	Reader *VictoriaMetricsReader `asm:"victoriametrics-reader,optional"`

	instance string
}

var _ = autoreg.Register[MemoryUsage]()

func (m *MemoryUsage) Populated() error {
	return m.Setup()
}

func (m *MemoryUsage) Setup() error {
	// Generate unique instance ID using ULID
	m.instance = ulid.MustNew(ulid.Now(), rand.Reader).String()

	m.Log.Info("memory usage metrics initialized with VictoriaMetrics backend", "instance", m.instance)
	return nil
}

func (m *MemoryUsage) RecordUsage(
	ctx context.Context,
	entity string,
	ts time.Time,
	memory units.Bytes,
	attrs map[string]string,
) error {
	if m.Writer == nil {
		return nil
	}

	// Build labels from attributes
	labels := make(map[string]string)
	labels["entity"] = entity
	labels["instance"] = m.instance
	for k, v := range attrs {
		labels[k] = v
	}

	// Write memory usage in bytes
	point := MetricPoint{
		Name:      "memory_usage_bytes",
		Labels:    labels,
		Value:     float64(memory.Int64()),
		Timestamp: ts,
	}

	return m.Writer.WritePoint(ctx, point)
}

type MemoryUsageAtTime struct {
	Timestamp time.Time
	Memory    units.Bytes
}

func (m *MemoryUsage) UsageLastHour(entity string) ([]MemoryUsageAtTime, error) {
	if m.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	// Align to minute boundaries for predictable evaluation points
	now := time.Now()
	end := time.Unix(now.Unix()/60*60, 0)
	start := end.Add(-1 * time.Hour)

	// Query max memory usage per minute over the last hour
	query := fmt.Sprintf(`max_over_time(memory_usage_bytes{entity="%s"}[1m])`, entity)
	result, err := m.Reader.RangeQuery(context.Background(), query, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query memory usage: %w", err)
	}

	var results []MemoryUsageAtTime
	if len(result.Data.Result) > 0 {
		for _, value := range result.Data.Result[0].Values {
			timestamp, _ := value[0].(float64)
			memoryStr, _ := value[1].(string)
			memoryBytes, _ := strconv.ParseFloat(memoryStr, 64)

			// Shift timestamp back 1 minute to represent bucket start time
			// (VictoriaMetrics returns the end of the measurement window)
			results = append(results, MemoryUsageAtTime{
				Timestamp: time.Unix(int64(timestamp), 0).Add(-1 * time.Minute),
				Memory:    units.Bytes(int64(memoryBytes)),
			})
		}
	}

	return results, nil
}
