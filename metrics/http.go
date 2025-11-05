package metrics

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"miren.dev/runtime/pkg/asm/autoreg"
)

// HTTPMetrics tracks HTTP request metrics for applications using VictoriaMetrics
type HTTPMetrics struct {
	Log    *slog.Logger
	Writer *VictoriaMetricsWriter `asm:"victoriametrics-writer,optional"`
	Reader *VictoriaMetricsReader `asm:"victoriametrics-reader,optional"`

	mu       sync.Mutex
	counters map[string]*counterState
	instance string
}

type counterState struct {
	requestCount  float64
	durationSum   float64
	durationCount float64
}

var _ = autoreg.Register[HTTPMetrics]()

func (h *HTTPMetrics) Populated() error {
	return h.Setup()
}

func (h *HTTPMetrics) Setup() error {
	// For VictoriaMetrics, we don't need to create tables/schemas
	// The metrics are created dynamically when first written
	h.counters = make(map[string]*counterState)

	// Generate unique instance ID using ULID
	h.instance = ulid.MustNew(ulid.Now(), rand.Reader).String()

	h.Log.Info("HTTP metrics initialized with VictoriaMetrics backend", "instance", h.instance)
	return nil
}

// HTTPRequest represents a single HTTP request for metrics
type HTTPRequest struct {
	Timestamp    time.Time
	App          string
	Method       string
	Path         string
	StatusCode   int
	DurationMs   int64
	ResponseSize int64
}

// RecordRequest records an HTTP request as metrics in VictoriaMetrics
func (h *HTTPMetrics) RecordRequest(ctx context.Context, req HTTPRequest) error {
	if h == nil || h.Writer == nil {
		return nil
	}

	// Create keys for counter lookups
	requestKey := fmt.Sprintf("%s:%s:%s:%d", req.App, req.Method, req.Path, req.StatusCode)
	durationKey := fmt.Sprintf("%s:%s:%s", req.App, req.Method, req.Path)

	h.mu.Lock()

	if h.counters == nil {
		h.counters = make(map[string]*counterState)
	}

	// Get or create request counter
	reqCounter, ok := h.counters[requestKey]
	if !ok {
		reqCounter = &counterState{}
		h.counters[requestKey] = reqCounter
	}
	reqCounter.requestCount++
	requestCount := reqCounter.requestCount

	// Get or create duration counter
	durCounter, ok := h.counters[durationKey]
	if !ok {
		durCounter = &counterState{}
		h.counters[durationKey] = durCounter
	}
	durCounter.durationSum += float64(req.DurationMs) / 1000.0 // Convert ms to seconds
	durCounter.durationCount++
	durationSum := durCounter.durationSum
	durationCount := durCounter.durationCount

	h.mu.Unlock()

	// Write cumulative counter values and individual duration sample
	points := []MetricPoint{
		{
			Name: "http_requests_total",
			Labels: map[string]string{
				"app":      req.App,
				"method":   req.Method,
				"path":     req.Path,
				"status":   strconv.Itoa(req.StatusCode),
				"instance": h.instance,
			},
			Value:     requestCount,
			Timestamp: req.Timestamp,
		},
		{
			Name: "http_request_duration_seconds_sum",
			Labels: map[string]string{
				"app":      req.App,
				"method":   req.Method,
				"path":     req.Path,
				"instance": h.instance,
			},
			Value:     durationSum,
			Timestamp: req.Timestamp,
		},
		{
			Name: "http_request_duration_seconds_count",
			Labels: map[string]string{
				"app":      req.App,
				"method":   req.Method,
				"path":     req.Path,
				"instance": h.instance,
			},
			Value:     durationCount,
			Timestamp: req.Timestamp,
		},
		{
			Name: "http_request_duration_seconds",
			Labels: map[string]string{
				"app":      req.App,
				"method":   req.Method,
				"path":     req.Path,
				"instance": h.instance,
			},
			Value:     float64(req.DurationMs) / 1000.0,
			Timestamp: req.Timestamp,
		},
	}

	return h.Writer.WritePoints(ctx, points)
}

// RPSLastMinute returns requests per second for the last minute
func (h *HTTPMetrics) RPSLastMinute(app string) (float64, error) {
	if h.Reader == nil {
		return 0, fmt.Errorf("reader not initialized")
	}

	query := fmt.Sprintf(`sum(rate(http_requests_total{app="%s"}[1m]))`, app)
	result, err := h.Reader.InstantQuery(context.Background(), query, time.Time{})
	if err != nil {
		return 0, fmt.Errorf("failed to query RPS: %w", err)
	}

	if len(result.Data.Result) == 0 {
		return 0, nil
	}

	valueStr, ok := result.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type")
	}

	rps, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse RPS value: %w", err)
	}

	return rps, nil
}

// RequestStats represents aggregated request statistics
type RequestStats struct {
	Time          time.Time
	Count         int64
	AvgDurationMs float64
	P95DurationMs float64
	P99DurationMs float64
	ErrorRate     float64
}

// StatsLastHour returns request statistics for the last hour in 1-minute buckets
func (h *HTTPMetrics) StatsLastHour(app string) ([]RequestStats, error) {
	if h.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	// Align to minute boundaries for predictable evaluation points
	now := time.Now()
	end := time.Unix(now.Unix()/60*60, 0)
	start := end.Add(-1 * time.Hour)

	// Query for count per minute
	countQuery := fmt.Sprintf(`sum(increase(http_requests_total{app="%s"}[1m]))`, app)
	countResult, err := h.Reader.RangeQuery(context.Background(), countQuery, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query count: %w", err)
	}

	// Query for average duration in milliseconds
	avgQuery := fmt.Sprintf(`(sum(rate(http_request_duration_seconds_sum{app="%s"}[1m])) / sum(rate(http_request_duration_seconds_count{app="%s"}[1m]))) * 1000`, app, app)
	avgResult, err := h.Reader.RangeQuery(context.Background(), avgQuery, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query avg duration: %w", err)
	}

	// Query for p95 duration in milliseconds
	p95Query := fmt.Sprintf(`quantile_over_time(0.95, http_request_duration_seconds{app="%s"}[1m]) * 1000`, app)
	p95Result, err := h.Reader.RangeQuery(context.Background(), p95Query, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query p95: %w", err)
	}

	// Query for p99 duration in milliseconds
	p99Query := fmt.Sprintf(`quantile_over_time(0.99, http_request_duration_seconds{app="%s"}[1m]) * 1000`, app)
	p99Result, err := h.Reader.RangeQuery(context.Background(), p99Query, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query p99: %w", err)
	}

	// Query for error rate
	errorQuery := fmt.Sprintf(`sum(rate(http_requests_total{app="%s",status=~"[45].."}[1m])) / sum(rate(http_requests_total{app="%s"}[1m]))`, app, app)
	errorResult, err := h.Reader.RangeQuery(context.Background(), errorQuery, start, end, "1m")
	if err != nil {
		return nil, fmt.Errorf("failed to query error rate: %w", err)
	}

	// Combine results
	stats := make([]RequestStats, 0)
	if len(countResult.Data.Result) > 0 {
		for _, value := range countResult.Data.Result[0].Values {
			timestamp, _ := value[0].(float64)
			countStr, _ := value[1].(string)
			count, _ := strconv.ParseInt(countStr, 10, 64)

			// Shift timestamp back 1 minute to represent bucket start time
			// (VictoriaMetrics returns the end of the measurement window)
			stat := RequestStats{
				Time:  time.Unix(int64(timestamp), 0).Add(-1 * time.Minute),
				Count: count,
			}

			// Find corresponding values from other queries
			if len(avgResult.Data.Result) > 0 {
				for _, v := range avgResult.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						avgStr, _ := v[1].(string)
						stat.AvgDurationMs, _ = strconv.ParseFloat(avgStr, 64)
						break
					}
				}
			}

			if len(p95Result.Data.Result) > 0 {
				for _, v := range p95Result.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						p95Str, _ := v[1].(string)
						stat.P95DurationMs, _ = strconv.ParseFloat(p95Str, 64)
						break
					}
				}
			}

			if len(p99Result.Data.Result) > 0 {
				for _, v := range p99Result.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						p99Str, _ := v[1].(string)
						stat.P99DurationMs, _ = strconv.ParseFloat(p99Str, 64)
						break
					}
				}
			}

			if len(errorResult.Data.Result) > 0 {
				for _, v := range errorResult.Data.Result[0].Values {
					t, _ := v[0].(float64)
					if int64(t) == int64(timestamp) {
						errStr, _ := v[1].(string)
						stat.ErrorRate, _ = strconv.ParseFloat(errStr, 64)
						break
					}
				}
			}

			stats = append(stats, stat)
		}
	}

	return stats, nil
}

// PathStats represents statistics for a specific path
type PathStats struct {
	Path          string
	Count         int64
	AvgDurationMs float64
	ErrorRate     float64
}

// TopPaths returns the most frequently accessed paths for an app
func (h *HTTPMetrics) TopPaths(app string, limit int) ([]PathStats, error) {
	if h.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	// Query for top paths by count
	query := fmt.Sprintf(`topk(%d, sum by(path) (increase(http_requests_total{app="%s"}[1h])))`, limit, app)
	result, err := h.Reader.InstantQuery(context.Background(), query, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("failed to query top paths: %w", err)
	}

	var paths []PathStats
	for _, r := range result.Data.Result {
		path := r.Metric["path"]
		countStr, _ := r.Value[1].(string)
		count, _ := strconv.ParseInt(countStr, 10, 64)

		// Query average duration for this path in milliseconds
		avgQuery := fmt.Sprintf(`(sum(rate(http_request_duration_seconds_sum{app="%s",path="%s"}[1h])) / sum(rate(http_request_duration_seconds_count{app="%s",path="%s"}[1h]))) * 1000`, app, path, app, path)
		avgResult, err := h.Reader.InstantQuery(context.Background(), avgQuery, time.Time{})
		var avgDuration float64
		if err == nil && len(avgResult.Data.Result) > 0 {
			avgStr, _ := avgResult.Data.Result[0].Value[1].(string)
			avgDuration, _ = strconv.ParseFloat(avgStr, 64)
		}

		// Query error rate for this path
		errorQuery := fmt.Sprintf(`sum(rate(http_requests_total{app="%s",path="%s",status=~"[45].."}[1h])) / sum(rate(http_requests_total{app="%s",path="%s"}[1h]))`, app, path, app, path)
		errorResult, err := h.Reader.InstantQuery(context.Background(), errorQuery, time.Time{})
		var errorRate float64
		if err == nil && len(errorResult.Data.Result) > 0 {
			errStr, _ := errorResult.Data.Result[0].Value[1].(string)
			errorRate, _ = strconv.ParseFloat(errStr, 64)
		}

		paths = append(paths, PathStats{
			Path:          path,
			Count:         count,
			AvgDurationMs: avgDuration,
			ErrorRate:     errorRate,
		})
	}

	return paths, nil
}

// ErrorBreakdown represents error counts by status code
type ErrorBreakdown struct {
	StatusCode int
	Count      int64
	Percentage float64
}

// ErrorsLastHour returns breakdown of errors by status code for the last hour
func (h *HTTPMetrics) ErrorsLastHour(app string) ([]ErrorBreakdown, error) {
	if h.Reader == nil {
		return nil, fmt.Errorf("reader not initialized")
	}

	// Query for errors grouped by status code
	query := fmt.Sprintf(`sum by(status) (increase(http_requests_total{app="%s",status=~"[45].."}[1h]))`, app)
	result, err := h.Reader.InstantQuery(context.Background(), query, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("failed to query errors: %w", err)
	}

	var errors []ErrorBreakdown
	var totalErrors int64

	for _, r := range result.Data.Result {
		statusStr := r.Metric["status"]
		status, _ := strconv.Atoi(statusStr)
		countStr, _ := r.Value[1].(string)
		count, _ := strconv.ParseInt(countStr, 10, 64)

		totalErrors += count
		errors = append(errors, ErrorBreakdown{
			StatusCode: status,
			Count:      count,
		})
	}

	// Calculate percentages
	if totalErrors > 0 {
		for i := range errors {
			errors[i].Percentage = float64(errors[i].Count) / float64(totalErrors) * 100
		}
	}

	return errors, nil
}

// Close is a no-op for VictoriaMetrics (writer handles its own lifecycle)
func (h *HTTPMetrics) Close() error {
	return nil
}
