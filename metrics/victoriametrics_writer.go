package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VictoriaMetricsWriter writes metrics to VictoriaMetrics using the import API
type VictoriaMetricsWriter struct {
	Log     *slog.Logger
	Address string // e.g., "localhost:8428"
	Timeout time.Duration

	buffer    []MetricPoint
	mu        sync.Mutex
	flushCtx  context.Context
	cancel    context.CancelFunc
	flushChan chan struct{}
	wg        sync.WaitGroup
	client    *http.Client
}

const (
	defaultMetricBufferSize  = 1000
	maxMetricBufferSize      = 10000 // Maximum buffer size before rejecting writes
	defaultMetricFlushPeriod = 5 * time.Second
)

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Name      string
	Labels    map[string]string
	Value     float64
	Timestamp time.Time
}

// NewVictoriaMetricsWriter creates a new VictoriaMetrics writer
func NewVictoriaMetricsWriter(log *slog.Logger, address string, timeout time.Duration) *VictoriaMetricsWriter {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	w := &VictoriaMetricsWriter{
		Log:       log,
		Address:   address,
		Timeout:   timeout,
		buffer:    make([]MetricPoint, 0, defaultMetricBufferSize),
		flushChan: make(chan struct{}, 1),
		client: &http.Client{
			Timeout: timeout,
		},
	}

	return w
}

// Start begins the background flush routine
func (w *VictoriaMetricsWriter) Start() {
	w.flushCtx, w.cancel = context.WithCancel(context.Background())
	w.wg.Add(1)
	go w.flushLoop()
}

// WritePoint adds a metric point to the buffer
func (w *VictoriaMetricsWriter) WritePoint(ctx context.Context, point MetricPoint) error {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	// Check if buffer would exceed maximum size
	if len(w.buffer) >= maxMetricBufferSize {
		w.mu.Unlock()
		return fmt.Errorf("metric buffer full (%d points), rejecting write to prevent OOM", maxMetricBufferSize)
	}
	w.buffer = append(w.buffer, point)
	needsFlush := len(w.buffer) >= defaultMetricBufferSize
	w.mu.Unlock()

	if needsFlush {
		select {
		case w.flushChan <- struct{}{}:
		default:
		}
	}

	return nil
}

// WritePoints adds multiple metric points to the buffer
func (w *VictoriaMetricsWriter) WritePoints(ctx context.Context, points []MetricPoint) error {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	// Check if buffer would exceed maximum size after appending
	if len(w.buffer)+len(points) > maxMetricBufferSize {
		w.mu.Unlock()
		return fmt.Errorf("metric buffer would exceed limit (%d + %d > %d), rejecting write to prevent OOM",
			len(w.buffer), len(points), maxMetricBufferSize)
	}
	w.buffer = append(w.buffer, points...)
	needsFlush := len(w.buffer) >= defaultMetricBufferSize
	w.mu.Unlock()

	if needsFlush {
		select {
		case w.flushChan <- struct{}{}:
		default:
		}
	}

	return nil
}

// flushLoop runs in the background to periodically flush the buffer
func (w *VictoriaMetricsWriter) flushLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(defaultMetricFlushPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.flush()
		case <-w.flushChan:
			w.flush()
		case <-w.flushCtx.Done():
			return
		}
	}
}

// flush writes buffered metrics to VictoriaMetrics
func (w *VictoriaMetricsWriter) flush() {
	w.mu.Lock()
	if len(w.buffer) == 0 {
		w.mu.Unlock()
		return
	}

	toFlush := w.buffer
	w.buffer = make([]MetricPoint, 0, defaultMetricBufferSize)
	w.mu.Unlock()

	err := w.sendMetrics(toFlush)
	if err != nil {
		w.mu.Lock()
		defer w.mu.Unlock()

		// Re-add failed metrics to the front of the buffer
		w.buffer = append(toFlush, w.buffer...)

		w.Log.Error("failed to send metrics to victoriametrics", "error", err, "count", len(toFlush))
	}
}

// sendMetrics sends metrics to VictoriaMetrics using the import API
func (w *VictoriaMetricsWriter) sendMetrics(points []MetricPoint) error {
	if len(points) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, point := range points {
		line := w.formatMetricLine(point)
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	url := fmt.Sprintf("http://%s/api/v1/import/prometheus", w.Address)
	req, err := http.NewRequestWithContext(context.Background(), "POST", url, &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "text/plain")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victoriametrics returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// formatMetricLine formats a metric point in Prometheus text format
// Format: metric_name{label1="value1",label2="value2"} value timestamp_ms
func (w *VictoriaMetricsWriter) formatMetricLine(point MetricPoint) string {
	var sb strings.Builder

	// Write metric name
	sb.WriteString(sanitizeMetricName(point.Name))

	// Write labels if any
	if len(point.Labels) > 0 {
		sb.WriteByte('{')
		first := true
		for k, v := range point.Labels {
			if !first {
				sb.WriteByte(',')
			}
			first = false
			sb.WriteString(sanitizeLabelName(k))
			sb.WriteString(`="`)
			sb.WriteString(escapeLabelValue(v))
			sb.WriteByte('"')
		}
		sb.WriteByte('}')
	}

	// Write value
	sb.WriteByte(' ')
	sb.WriteString(strconv.FormatFloat(point.Value, 'f', -1, 64))

	// Write timestamp in milliseconds
	sb.WriteByte(' ')
	sb.WriteString(strconv.FormatInt(point.Timestamp.UnixMilli(), 10))

	return sb.String()
}

// Flush manually triggers a flush of buffered metrics
func (w *VictoriaMetricsWriter) Flush() {
	if w == nil {
		return
	}
	w.flush()
}

// Close stops the background flush routine and flushes remaining data
func (w *VictoriaMetricsWriter) Close() error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	w.flush()
	return nil
}

// Helper functions for sanitizing metric and label names

func sanitizeMetricName(name string) string {
	var sb strings.Builder
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == ':' || (i > 0 && r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

func sanitizeLabelName(name string) string {
	var sb strings.Builder
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return value
}
