package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/units"
)

type CPUUsage struct {
	Log    *slog.Logger
	Writer *VictoriaMetricsWriter `asm:"victoriametrics-writer,optional"`
	Reader *VictoriaMetricsReader `asm:"victoriametrics-reader,optional"`
}

var _ = autoreg.Register[CPUUsage]()

func (m *CPUUsage) Populated() error {
	return m.Setup()
}

func (m *CPUUsage) Setup() error {
	m.Log.Info("CPU usage metrics initialized with VictoriaMetrics backend")
	return nil
}

func (m *CPUUsage) RecordUsage(ctx context.Context, entity string, windowStart, windowEnd time.Time, cpuUsec units.Microseconds, attrs map[string]string) error {
	if m.Writer == nil {
		return nil
	}

	// Calculate CPU cores used during this window
	windowDurationUsec := windowEnd.Sub(windowStart).Microseconds()
	if windowDurationUsec == 0 {
		return nil
	}

	// cores = cpu_usec / window_duration_usec
	cores := float64(cpuUsec) / float64(windowDurationUsec)

	// Build labels from attributes
	labels := make(map[string]string)
	labels["entity"] = entity
	for k, v := range attrs {
		labels[k] = v
	}

	// Write CPU usage in cores
	point := MetricPoint{
		Name:      "cpu_usage_cores",
		Labels:    labels,
		Value:     cores,
		Timestamp: windowEnd,
	}

	return m.Writer.WritePoint(ctx, point)
}

type UsageAtTime struct {
	Timestamp time.Time
	Cores     float64
}

func (m *CPUUsage) CPUUsageLastHour(entity string) ([]UsageAtTime, error) {
	if m.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	now := time.Now()
	start := now.Add(-1 * time.Hour)

	// Query average CPU cores per minute over the last hour
	query := fmt.Sprintf(`avg_over_time(cpu_usage_cores{entity="%s"}[1m])`, entity)
	result, err := m.Reader.RangeQuery(context.Background(), query, start, now, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query CPU usage: %w", err)
	}

	var results []UsageAtTime
	if len(result.Data.Result) > 0 {
		for _, value := range result.Data.Result[0].Values {
			timestamp, _ := value[0].(float64)
			coresStr, _ := value[1].(string)
			cores, _ := strconv.ParseFloat(coresStr, 64)

			results = append(results, UsageAtTime{
				Timestamp: time.Unix(int64(timestamp), 0),
				Cores:     cores,
			})
		}
	}

	return results, nil
}

// Returns the cpu usage in cores for the given entity over a given interval
func (m *CPUUsage) CPUUsageOver(entity string, interval string) (float64, error) {
	if m.Reader == nil {
		return 0, fmt.Errorf("reader not initialized")
	}

	// Use avg_over_time to get average cores over the interval
	query := fmt.Sprintf(`avg_over_time(cpu_usage_cores{entity="%s"}[%s])`, entity, interval)
	result, err := m.Reader.InstantQuery(context.Background(), query, time.Time{})
	if err != nil {
		return 0, fmt.Errorf("failed to query CPU usage: %w", err)
	}

	if len(result.Data.Result) == 0 {
		return 0, nil
	}

	coresStr, ok := result.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type")
	}

	cores, err := strconv.ParseFloat(coresStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cores value: %w", err)
	}

	return cores, nil
}

// Returns the cpu usage in cores for the given entity in the last minute
func (m *CPUUsage) CurrentCPUUsage(entity string) (float64, error) {
	return m.CPUUsageOver(entity, "1m")
}

func (m *CPUUsage) CPUUsageOverLastHour(entity string) (float64, error) {
	return m.CPUUsageOver(entity, "1h")
}

func (m *CPUUsage) CPUUsageOverDay(entity string) (float64, error) {
	return m.CPUUsageOver(entity, "24h")
}

// Returns the cpu usage in cores for the given entity for a specific day in the past
func (m *CPUUsage) CPUUsageDayAgo(entity string, day units.Days) (float64, error) {
	if m.Reader == nil {
		return 0, fmt.Errorf("reader not initialized")
	}

	// Calculate the time range for the specific day
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -int(day))
	dayEnd := dayStart.Add(24 * time.Hour)

	// Query average CPU cores for that specific day
	query := fmt.Sprintf(`avg_over_time(cpu_usage_cores{entity="%s"}[24h])`, entity)
	result, err := m.Reader.InstantQuery(context.Background(), query, dayEnd)
	if err != nil {
		return 0, fmt.Errorf("failed to query CPU usage: %w", err)
	}

	if len(result.Data.Result) == 0 {
		return 0, nil
	}

	coresStr, ok := result.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type")
	}

	cores, err := strconv.ParseFloat(coresStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cores value: %w", err)
	}

	return cores, nil
}
