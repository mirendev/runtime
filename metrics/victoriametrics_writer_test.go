package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVictoriaMetricsWriter_WritePoint(t *testing.T) {
	t.Run("writes single point successfully", func(t *testing.T) {
		var receivedData string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			receivedData = string(body)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		point := MetricPoint{
			Name: "test_metric",
			Labels: map[string]string{
				"app":    "test",
				"status": "200",
			},
			Value:     42.5,
			Timestamp: time.Unix(1234567890, 0),
		}

		err := writer.WritePoint(context.Background(), point)
		require.NoError(t, err)

		// Manually flush to trigger HTTP request
		writer.flush()

		// Verify Prometheus format
		assert.Contains(t, receivedData, "test_metric{")
		assert.Contains(t, receivedData, `app="test"`)
		assert.Contains(t, receivedData, `status="200"`)
		assert.Contains(t, receivedData, "42.5")
		assert.Contains(t, receivedData, "1234567890000") // timestamp in milliseconds
	})

	t.Run("rejects write when buffer is full", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, "localhost:8428", 10*time.Second)

		// Fill buffer to maxMetricBufferSize
		for i := 0; i < maxMetricBufferSize; i++ {
			err := writer.WritePoint(context.Background(), MetricPoint{
				Name:      "test_metric",
				Value:     float64(i),
				Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}

		// Next write should fail
		err := writer.WritePoint(context.Background(), MetricPoint{
			Name:      "test_metric",
			Value:     999,
			Timestamp: time.Now(),
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metric buffer full")
		assert.Contains(t, err.Error(), "rejecting write to prevent OOM")
	})

	t.Run("handles nil writer gracefully", func(t *testing.T) {
		var writer *VictoriaMetricsWriter
		err := writer.WritePoint(context.Background(), MetricPoint{Name: "test", Value: 1, Timestamp: time.Now()})
		assert.NoError(t, err)
	})
}

func TestVictoriaMetricsWriter_WritePoints(t *testing.T) {
	t.Run("writes multiple points successfully", func(t *testing.T) {
		var receivedData string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			receivedData = string(body)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		points := []MetricPoint{
			{Name: "metric1", Value: 1, Timestamp: time.Unix(1000, 0)},
			{Name: "metric2", Value: 2, Timestamp: time.Unix(2000, 0)},
			{Name: "metric3", Value: 3, Timestamp: time.Unix(3000, 0)},
		}

		err := writer.WritePoints(context.Background(), points)
		require.NoError(t, err)

		writer.flush()

		// Verify all metrics are present
		lines := strings.Split(strings.TrimSpace(receivedData), "\n")
		assert.Len(t, lines, 3)
		assert.Contains(t, receivedData, "metric1 1 1000000")
		assert.Contains(t, receivedData, "metric2 2 2000000")
		assert.Contains(t, receivedData, "metric3 3 3000000")
	})

	t.Run("rejects write when batch would exceed buffer limit", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, "localhost:8428", 10*time.Second)

		// Fill buffer to near capacity
		for i := 0; i < maxMetricBufferSize-100; i++ {
			err := writer.WritePoint(context.Background(), MetricPoint{
				Name:      "test",
				Value:     float64(i),
				Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}

		// Try to write 200 more points (would exceed limit)
		bigBatch := make([]MetricPoint, 200)
		for i := range bigBatch {
			bigBatch[i] = MetricPoint{Name: "test", Value: float64(i), Timestamp: time.Now()}
		}

		err := writer.WritePoints(context.Background(), bigBatch)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metric buffer would exceed limit")
		assert.Contains(t, err.Error(), "rejecting write to prevent OOM")
	})
}

func TestVictoriaMetricsWriter_MetricSanitization(t *testing.T) {
	tests := []struct {
		name           string
		metricName     string
		labels         map[string]string
		expectedMetric string
		expectedLabels []string
	}{
		{
			name:           "metric name with dots",
			metricName:     "http.request.duration",
			labels:         map[string]string{},
			expectedMetric: "http_request_duration",
			expectedLabels: []string{},
		},
		{
			name:           "metric name with dashes",
			metricName:     "http-request-count",
			labels:         map[string]string{},
			expectedMetric: "http_request_count",
			expectedLabels: []string{},
		},
		{
			name:           "label name with dashes",
			metricName:     "test",
			labels:         map[string]string{"app-name": "test"},
			expectedMetric: "test{",
			expectedLabels: []string{`app_name="test"`},
		},
		{
			name:           "label value with quotes and backslashes",
			metricName:     "test",
			labels:         map[string]string{"path": `/api/test"with\quotes`},
			expectedMetric: "test{",
			expectedLabels: []string{`path="/api/test\"with\\quotes"`},
		},
		{
			name:           "label value with newlines",
			metricName:     "test",
			labels:         map[string]string{"message": "line1\nline2"},
			expectedMetric: "test{",
			expectedLabels: []string{`message="line1\nline2"`},
		},
		{
			name:           "metric name starting with number",
			metricName:     "123_metric",
			labels:         map[string]string{},
			expectedMetric: "_23_metric", // First char becomes _, but 2 and 3 are valid in positions > 0
			expectedLabels: []string{},
		},
		{
			name:           "metric name with colon (allowed)",
			metricName:     "http:request:total",
			labels:         map[string]string{},
			expectedMetric: "http:request:total",
			expectedLabels: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedData string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				receivedData = string(body)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

			point := MetricPoint{
				Name:      tt.metricName,
				Labels:    tt.labels,
				Value:     1,
				Timestamp: time.Unix(1000, 0),
			}

			err := writer.WritePoint(context.Background(), point)
			require.NoError(t, err)

			writer.flush()

			assert.Contains(t, receivedData, tt.expectedMetric)
			for _, expectedLabel := range tt.expectedLabels {
				assert.Contains(t, receivedData, expectedLabel)
			}
		})
	}
}

func TestVictoriaMetricsWriter_FlushBehavior(t *testing.T) {
	t.Run("automatically flushes when buffer reaches 1000 points", func(t *testing.T) {
		flushCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flushCount++
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
		writer.Start()
		defer writer.Close()

		// Write exactly 1000 points
		for i := 0; i < 1000; i++ {
			err := writer.WritePoint(context.Background(), MetricPoint{
				Name:      "test",
				Value:     float64(i),
				Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}

		// Wait for async flush
		time.Sleep(500 * time.Millisecond)

		assert.Equal(t, 1, flushCount, "should have flushed once after 1000 points")
	})

	t.Run("periodically flushes every 5 seconds", func(t *testing.T) {
		flushCount := 0
		var mu sync.Mutex
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			flushCount++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
		writer.Start()
		defer writer.Close()

		// Write a few points
		for i := 0; i < 10; i++ {
			err := writer.WritePoint(context.Background(), MetricPoint{
				Name:      "test",
				Value:     float64(i),
				Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}

		// Wait for periodic flush (5 seconds)
		time.Sleep(6 * time.Second)

		mu.Lock()
		count := flushCount
		mu.Unlock()

		assert.GreaterOrEqual(t, count, 1, "should have flushed at least once in 6 seconds")
	})

	t.Run("manual flush works", func(t *testing.T) {
		flushCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flushCount++
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		// Write a few points
		for i := 0; i < 5; i++ {
			err := writer.WritePoint(context.Background(), MetricPoint{
				Name:      "test",
				Value:     float64(i),
				Timestamp: time.Now(),
			})
			require.NoError(t, err)
		}

		// Manual flush
		writer.flush()

		assert.Equal(t, 1, flushCount)
	})

	t.Run("flush does nothing with empty buffer", func(t *testing.T) {
		flushCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flushCount++
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		// Flush empty buffer
		writer.flush()

		assert.Equal(t, 0, flushCount)
	})
}

func TestVictoriaMetricsWriter_Close(t *testing.T) {
	t.Run("flushes remaining buffer on close", func(t *testing.T) {
		var receivedData string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			receivedData = string(body)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
		writer.Start()

		// Write some points
		for i := 0; i < 5; i++ {
			err := writer.WritePoint(context.Background(), MetricPoint{
				Name:      "test",
				Value:     float64(i),
				Timestamp: time.Unix(int64(1000+i), 0),
			})
			require.NoError(t, err)
		}

		// Close should flush
		err := writer.Close()
		require.NoError(t, err)

		// Verify all 5 metrics were flushed
		lines := strings.Split(strings.TrimSpace(receivedData), "\n")
		assert.Len(t, lines, 5)
	})

	t.Run("stops background flush routine", func(t *testing.T) {
		flushCount := 0
		var mu sync.Mutex
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			flushCount++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
		writer.Start()

		// Close immediately
		err := writer.Close()
		require.NoError(t, err)

		// Wait to see if background routine continues (it shouldn't)
		time.Sleep(2 * time.Second)

		mu.Lock()
		count := flushCount
		mu.Unlock()

		assert.Equal(t, 0, count, "background routine should not flush after close")
	})

	t.Run("close is idempotent", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, "localhost:8428", 10*time.Second)
		writer.Start()

		err1 := writer.Close()
		require.NoError(t, err1)

		err2 := writer.Close()
		require.NoError(t, err2)
	})
}

func TestVictoriaMetricsWriter_ConcurrentWrites(t *testing.T) {
	t.Run("handles concurrent writes safely", func(t *testing.T) {
		var receivedData string
		var mu sync.Mutex
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			receivedData += string(body)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		// Concurrent writes from multiple goroutines
		var wg sync.WaitGroup
		numGoroutines := 10
		pointsPerGoroutine := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < pointsPerGoroutine; j++ {
					err := writer.WritePoint(context.Background(), MetricPoint{
						Name:      "concurrent_test",
						Labels:    map[string]string{"goroutine": string(rune(id + '0'))},
						Value:     float64(j),
						Timestamp: time.Now(),
					})
					if err != nil {
						t.Logf("write error: %v", err)
					}
				}
			}(i)
		}

		wg.Wait()

		// Flush all remaining
		writer.flush()

		// Verify we got the expected number of metrics
		mu.Lock()
		data := receivedData
		mu.Unlock()

		lines := strings.Split(strings.TrimSpace(data), "\n")
		expectedLines := numGoroutines * pointsPerGoroutine
		assert.Equal(t, expectedLines, len(lines), "should receive all metrics from concurrent writes")
	})
}

func TestVictoriaMetricsWriter_ErrorHandling(t *testing.T) {
	t.Run("handles 500 error from server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		err := writer.WritePoint(context.Background(), MetricPoint{
			Name:      "test",
			Value:     1,
			Timestamp: time.Now(),
		})
		require.NoError(t, err) // WritePoint doesn't return flush errors

		// Flush will log error but not panic
		writer.flush()
	})

	t.Run("handles server timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second) // Longer than client timeout
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 500*time.Millisecond)

		err := writer.WritePoint(context.Background(), MetricPoint{
			Name:      "test",
			Value:     1,
			Timestamp: time.Now(),
		})
		require.NoError(t, err)

		// Flush will timeout and log error
		writer.flush()
	})

	t.Run("handles 400 bad request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad request: invalid metric format"))
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		err := writer.WritePoint(context.Background(), MetricPoint{
			Name:      "test",
			Value:     1,
			Timestamp: time.Now(),
		})
		require.NoError(t, err)

		// Flush will log error
		writer.flush()
	})
}

func TestVictoriaMetricsWriter_PrometheusFormat(t *testing.T) {
	t.Run("generates correct prometheus format", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, "localhost:8428", 10*time.Second)

		point := MetricPoint{
			Name: "http_requests_total",
			Labels: map[string]string{
				"method": "GET",
				"path":   "/api/v1/users",
				"status": "200",
			},
			Value:     1234,
			Timestamp: time.Unix(1609459200, 0), // 2021-01-01 00:00:00 UTC
		}

		line := writer.formatMetricLine(point)

		// Should contain metric name
		assert.Contains(t, line, "http_requests_total")

		// Should contain all labels
		assert.Contains(t, line, `method="GET"`)
		assert.Contains(t, line, `path="/api/v1/users"`)
		assert.Contains(t, line, `status="200"`)

		// Should contain value
		assert.Contains(t, line, "1234")

		// Should contain timestamp in milliseconds
		assert.Contains(t, line, "1609459200000")
	})

	t.Run("handles metric without labels", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, "localhost:8428", 10*time.Second)

		point := MetricPoint{
			Name:      "up",
			Labels:    map[string]string{},
			Value:     1,
			Timestamp: time.Unix(1000, 0),
		}

		line := writer.formatMetricLine(point)

		assert.Equal(t, "up 1 1000000", line)
	})

	t.Run("handles floating point values correctly", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		writer := NewVictoriaMetricsWriter(log, "localhost:8428", 10*time.Second)

		point := MetricPoint{
			Name:      "temperature",
			Value:     23.456789,
			Timestamp: time.Unix(1000, 0),
		}

		line := writer.formatMetricLine(point)

		assert.Contains(t, line, "temperature 23.456789 1000000")
	})
}
