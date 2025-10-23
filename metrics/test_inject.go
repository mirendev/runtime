package metrics

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/pkg/asm"
)

// TestInject provides VictoriaMetrics writer and reader for test environments using a mock HTTP server
func TestInject(reg *asm.Registry) {
	// Create a mock VictoriaMetrics HTTP server
	var writtenMetrics []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/import/prometheus") {
			// Handle metric writes
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			writtenMetrics = append(writtenMetrics, string(body))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		} else if strings.Contains(r.URL.Path, "/query_range") {
			// Handle range queries
			query := r.URL.Query().Get("query")
			var result QueryResult
			result.Status = "success"
			result.Data.ResultType = "matrix"

			baseTime := time.Now().Add(-1 * time.Hour).Unix()

			if strings.Contains(query, "cpu_usage_cores") {
				result.Data.Result = []Result{
					{
						Metric: map[string]string{},
						Values: [][]interface{}{
							{float64(baseTime), "1.5"},
							{float64(baseTime + 60), "1.2"},
							{float64(baseTime + 120), "1.8"},
						},
					},
				}
			} else if strings.Contains(query, "memory_usage_bytes") {
				result.Data.Result = []Result{
					{
						Metric: map[string]string{},
						Values: [][]interface{}{
							{float64(baseTime), "1048576"},
							{float64(baseTime + 60), "2097152"},
							{float64(baseTime + 120), "1572864"},
						},
					},
				}
			}

			json.NewEncoder(w).Encode(result)
		} else if strings.Contains(r.URL.Path, "/query") {
			// Handle instant queries
			query := r.URL.Query().Get("query")
			var result QueryResult
			result.Status = "success"
			result.Data.ResultType = "vector"

			if strings.Contains(query, "cpu_usage_cores") {
				// Return a realistic CPU usage value (> 0.5 cores to satisfy test)
				result.Data.Result = []Result{
					{
						Metric: map[string]string{},
						Value:  []interface{}{float64(time.Now().Unix()), "1.2"},
					},
				}
			} else if strings.Contains(query, "memory_usage_bytes") {
				result.Data.Result = []Result{
					{
						Metric: map[string]string{},
						Value:  []interface{}{float64(time.Now().Unix()), "1572864"},
					},
				}
			} else {
				// Return empty result for unknown queries
				result.Data.Result = []Result{}
			}

			json.NewEncoder(w).Encode(result)
		}
	}))

	// Extract host from server URL (removes http://)
	endpoint := strings.TrimPrefix(server.URL, "http://")

	// Provide a test VictoriaMetrics writer
	reg.ProvideName("victoriametrics-writer", func(opts struct {
		Log *slog.Logger
	}) *VictoriaMetricsWriter {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		writer := NewVictoriaMetricsWriter(log, endpoint, 30*time.Second)
		writer.Start()
		return writer
	})

	// Provide a test VictoriaMetrics reader
	reg.ProvideName("victoriametrics-reader", func(opts struct {
		Log *slog.Logger
	}) *VictoriaMetricsReader {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		return NewVictoriaMetricsReader(log, endpoint, 30*time.Second)
	})
}
