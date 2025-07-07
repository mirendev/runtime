package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/pkg/asm/autoreg"
)

// HTTPMetrics tracks HTTP request metrics for applications
type HTTPMetrics struct {
	Log *slog.Logger
	DB  *sql.DB `asm:"clickhouse"`

	// Buffering
	buffer    []HTTPRequest
	mu        sync.Mutex
	flushCtx  context.Context
	cancel    context.CancelFunc
	flushChan chan struct{} // Signal channel for flush requests
	wg        sync.WaitGroup
}

const defaultBufferSize = 1000

var _ = autoreg.Register[HTTPMetrics]()

func (h *HTTPMetrics) Populated() error {
	return h.Setup()
}

func (h *HTTPMetrics) Setup() error {
	_, err := h.DB.Exec(`
CREATE TABLE IF NOT EXISTS http_requests (
    timestamp DateTime64(6) CODEC(Delta(8), ZSTD(1)),
    app LowCardinality(String) CODEC(ZSTD(1)),
    method LowCardinality(String) CODEC(ZSTD(1)),
    path String CODEC(ZSTD(1)),
    status_code UInt16 CODEC(T64, ZSTD(1)),
    duration_ms UInt32 CODEC(T64, ZSTD(1)),
    response_size UInt64 CODEC(T64, ZSTD(1)),
    INDEX idx_path path TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
) 
ENGINE = MergeTree
ORDER BY (app, toUnixTimestamp(timestamp))
TTL toDateTime(timestamp) + INTERVAL 7 DAY
SETTINGS ttl_only_drop_parts = 1;
`)
	if err != nil {
		return err
	}

	// Initialize buffer and channels
	h.buffer = make([]HTTPRequest, 0, defaultBufferSize)
	h.flushChan = make(chan struct{}, 1) // Buffered to avoid blocking

	// Start background flush routine
	h.flushCtx, h.cancel = context.WithCancel(context.Background())
	h.wg.Add(1)
	go h.flushLoop()

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

// RecordRequest adds a request to the buffer for async processing
func (h *HTTPMetrics) RecordRequest(ctx context.Context, req HTTPRequest) error {
	if h == nil || h.DB == nil {
		return nil
	}

	h.mu.Lock()
	h.buffer = append(h.buffer, req)
	needsFlush := len(h.buffer) >= defaultBufferSize
	h.mu.Unlock()

	// Signal flush if buffer is full (non-blocking)
	if needsFlush {
		select {
		case h.flushChan <- struct{}{}:
			// Signaled flush
		default:
			// Channel full, flush already pending
		}
	}

	return nil
}

// flushLoop runs in the background to periodically flush the buffer
func (h *HTTPMetrics) flushLoop() {
	defer h.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.flush()
		case <-h.flushChan:
			h.flush()
		case <-h.flushCtx.Done():
			return
		}
	}
}

// flush writes buffered requests to ClickHouse
func (h *HTTPMetrics) flush() {
	h.mu.Lock()
	if len(h.buffer) == 0 {
		h.mu.Unlock()
		return
	}

	// Swap buffer
	toFlush := h.buffer
	h.buffer = make([]HTTPRequest, 0, 1000)
	h.mu.Unlock()

	// Batch insert
	tx, err := h.DB.Begin()
	if err != nil {
		h.Log.Error("Failed to begin transaction", "error", err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO http_requests (
			timestamp, app, method, path, status_code, duration_ms, response_size
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		h.Log.Error("Failed to prepare statement", "error", err)
		return
	}
	defer stmt.Close()

	for _, req := range toFlush {
		_, err = stmt.Exec(
			req.Timestamp,
			req.App,
			req.Method,
			req.Path,
			req.StatusCode,
			req.DurationMs,
			req.ResponseSize,
		)
		if err != nil {
			h.Log.Error("Failed to insert request", "error", err, "app", req.App)
		}
	}

	if err = tx.Commit(); err != nil {
		h.Log.Error("Failed to commit transaction", "error", err)
	} else {
		h.Log.Debug("Flushed HTTP metrics", "count", len(toFlush))
	}
}

// RPSLastMinute returns requests per second for the last minute
func (h *HTTPMetrics) RPSLastMinute(app string) (float64, error) {
	query := `
		SELECT count(*) / 60.0 as rps
		FROM http_requests
		WHERE app = ? AND timestamp > now() - INTERVAL 1 MINUTE
	`

	var rps float64
	err := h.DB.QueryRow(query, app).Scan(&rps)
	if err != nil {
		return 0, fmt.Errorf("failed to query RPS: %w", err)
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
	query := `
		SELECT
			toStartOfMinute(timestamp) as minute,
			count(*) as count,
			avg(duration_ms) as avg_duration,
			quantile(0.95)(duration_ms) as p95_duration,
			quantile(0.99)(duration_ms) as p99_duration,
			sum(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) / count(*) as error_rate
		FROM http_requests
		WHERE app = ? AND timestamp > now() - INTERVAL 1 HOUR
		GROUP BY minute
		ORDER BY minute
	`

	rows, err := h.DB.Query(query, app)
	if err != nil {
		return nil, fmt.Errorf("failed to query request stats: %w", err)
	}
	defer rows.Close()

	var stats []RequestStats
	for rows.Next() {
		var s RequestStats
		err := rows.Scan(&s.Time, &s.Count, &s.AvgDurationMs, &s.P95DurationMs, &s.P99DurationMs, &s.ErrorRate)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request stats: %w", err)
		}
		stats = append(stats, s)
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
	query := `
		SELECT
			path,
			count(*) as count,
			avg(duration_ms) as avg_duration,
			sum(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) / count(*) as error_rate
		FROM http_requests
		WHERE app = ? AND timestamp > now() - INTERVAL 1 HOUR
		GROUP BY path
		ORDER BY count DESC
		LIMIT ?
	`

	rows, err := h.DB.Query(query, app, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top paths: %w", err)
	}
	defer rows.Close()

	var paths []PathStats
	for rows.Next() {
		var p PathStats
		err := rows.Scan(&p.Path, &p.Count, &p.AvgDurationMs, &p.ErrorRate)
		if err != nil {
			return nil, fmt.Errorf("failed to scan path stats: %w", err)
		}
		paths = append(paths, p)
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
	query := `
		SELECT
			status_code,
			count(*) as count
		FROM http_requests
		WHERE app = ? AND timestamp > now() - INTERVAL 1 HOUR AND status_code >= 400
		GROUP BY status_code
		ORDER BY count DESC
	`

	rows, err := h.DB.Query(query, app)
	if err != nil {
		return nil, fmt.Errorf("failed to query error breakdown: %w", err)
	}
	defer rows.Close()

	var errors []ErrorBreakdown
	var totalErrors int64

	// First pass to collect data and count total
	for rows.Next() {
		var e ErrorBreakdown
		err := rows.Scan(&e.StatusCode, &e.Count)
		if err != nil {
			return nil, fmt.Errorf("failed to scan error breakdown: %w", err)
		}
		totalErrors += e.Count
		errors = append(errors, e)
	}

	// Calculate percentages
	if totalErrors > 0 {
		for i := range errors {
			errors[i].Percentage = float64(errors[i].Count) / float64(totalErrors) * 100
		}
	}

	return errors, nil
}

// Close stops the background flush routine and flushes remaining data
func (h *HTTPMetrics) Close() error {
	if h.cancel != nil {
		h.cancel()
	}
	// Wait for the flush loop to finish
	h.wg.Wait()
	// Final flush for any remaining data
	h.flush()
	return nil
}
