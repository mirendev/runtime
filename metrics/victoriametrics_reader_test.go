package metrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVictoriaMetricsReader_InstantQuery(t *testing.T) {
	t.Run("executes instant query successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/query", r.URL.Path)
			assert.Equal(t, "up", r.URL.Query().Get("query"))

			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "vector",
					Result: []Result{
						{
							Metric: map[string]string{"job": "prometheus"},
							Value:  []interface{}{float64(1609459200), "1"},
						},
					},
				},
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		result, err := reader.InstantQuery(context.Background(), "up", time.Time{})
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "vector", result.Data.ResultType)
		assert.Len(t, result.Data.Result, 1)
		assert.Equal(t, "prometheus", result.Data.Result[0].Metric["job"])
	})

	t.Run("includes timestamp when provided", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "1609459200", r.URL.Query().Get("time"))

			response := QueryResult{Status: "success", Data: Data{ResultType: "vector", Result: []Result{}}}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		ts := time.Unix(1609459200, 0)
		_, err := reader.InstantQuery(context.Background(), "up", ts)
		require.NoError(t, err)
	})

	t.Run("handles HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		_, err := reader.InstantQuery(context.Background(), "up", time.Time{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "victoriametrics returned status 500")
	})

	t.Run("handles malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not valid json"))
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		_, err := reader.InstantQuery(context.Background(), "up", time.Time{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})

	t.Run("handles query failure status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{Status: "error", Data: Data{}}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		_, err := reader.InstantQuery(context.Background(), "up", time.Time{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "query failed with status: error")
	})
}

func TestVictoriaMetricsReader_RangeQuery(t *testing.T) {
	t.Run("executes range query successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/query_range", r.URL.Path)
			assert.Equal(t, "up", r.URL.Query().Get("query"))
			assert.Equal(t, "1609459200", r.URL.Query().Get("start"))
			assert.Equal(t, "1609462800", r.URL.Query().Get("end"))
			assert.Equal(t, "1m", r.URL.Query().Get("step"))

			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "matrix",
					Result: []Result{
						{
							Metric: map[string]string{"job": "prometheus"},
							Values: [][]interface{}{
								{float64(1609459200), "1"},
								{float64(1609459260), "1"},
								{float64(1609459320), "1"},
							},
						},
					},
				},
			}

			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		start := time.Unix(1609459200, 0)
		end := time.Unix(1609462800, 0)

		result, err := reader.RangeQuery(context.Background(), "up", start, end, "1m")
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "matrix", result.Data.ResultType)
		assert.Len(t, result.Data.Result, 1)
		assert.Len(t, result.Data.Result[0].Values, 3)
	})

	t.Run("handles empty step parameter", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.URL.Query().Get("step"))
			response := QueryResult{Status: "success", Data: Data{ResultType: "matrix", Result: []Result{}}}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		start := time.Unix(1609459200, 0)
		end := time.Unix(1609462800, 0)

		_, err := reader.RangeQuery(context.Background(), "up", start, end, "")
		require.NoError(t, err)
	})

	t.Run("handles HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid query"))
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		_, err := reader.RangeQuery(context.Background(), "invalid query", start, end, "1m")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "victoriametrics returned status 400")
	})
}

func TestVictoriaMetricsReader_GetLatestValue(t *testing.T) {
	t.Run("retrieves latest value successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "vector",
					Result: []Result{
						{
							Metric: map[string]string{"__name__": "test_metric", "app": "test"},
							Value:  []interface{}{float64(1609459200), "42.5"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		value, err := reader.GetLatestValue(context.Background(), "test_metric", map[string]string{"app": "test"})
		require.NoError(t, err)
		assert.Equal(t, 42.5, value)
	})

	t.Run("returns error when no data found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{
				Status: "success",
				Data:   Data{ResultType: "vector", Result: []Result{}},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		_, err := reader.GetLatestValue(context.Background(), "nonexistent_metric", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no data found for metric")
	})

	t.Run("handles invalid value type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "vector",
					Result: []Result{
						{
							Metric: map[string]string{"__name__": "test"},
							Value:  []interface{}{float64(1609459200), 12345}, // int instead of string
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		_, err := reader.GetLatestValue(context.Background(), "test", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected value type")
	})
}

func TestVictoriaMetricsReader_GetTimeSeries(t *testing.T) {
	t.Run("retrieves time series successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "matrix",
					Result: []Result{
						{
							Metric: map[string]string{"__name__": "test_metric"},
							Values: [][]interface{}{
								{float64(1000), "10.5"},
								{float64(2000), "20.5"},
								{float64(3000), "30.5"},
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		start := time.Unix(0, 0)
		end := time.Unix(4000, 0)

		points, err := reader.GetTimeSeries(context.Background(), "test_metric", nil, start, end, "1s")
		require.NoError(t, err)
		require.Len(t, points, 3)

		assert.Equal(t, time.Unix(1000, 0), points[0].Timestamp)
		assert.Equal(t, 10.5, points[0].Value)
		assert.Equal(t, time.Unix(2000, 0), points[1].Timestamp)
		assert.Equal(t, 20.5, points[1].Value)
		assert.Equal(t, time.Unix(3000, 0), points[2].Timestamp)
		assert.Equal(t, 30.5, points[2].Value)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{
				Status: "success",
				Data:   Data{ResultType: "matrix", Result: []Result{}},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		points, err := reader.GetTimeSeries(context.Background(), "nonexistent", nil, start, end, "1m")
		require.NoError(t, err)
		assert.Empty(t, points)
	})

	t.Run("skips invalid data points", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "matrix",
					Result: []Result{
						{
							Metric: map[string]string{"__name__": "test"},
							Values: [][]interface{}{
								{float64(1000), "10.5"},
								{"invalid", "20.5"},        // invalid timestamp
								{float64(3000), 30.5},      // invalid value (int)
								{float64(4000), "40.5"},    // valid
								{float64(5000), "invalid"}, // invalid value
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		start := time.Unix(0, 0)
		end := time.Unix(6000, 0)

		points, err := reader.GetTimeSeries(context.Background(), "test", nil, start, end, "1s")
		require.NoError(t, err)
		// Should only get the 2 valid points
		assert.Len(t, points, 2)
	})
}

func TestVictoriaMetricsReader_GetRate(t *testing.T) {
	t.Run("calculates rate successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query().Get("query")
			assert.Contains(t, query, "rate(")
			assert.Contains(t, query, "[5m]")

			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "vector",
					Result: []Result{
						{
							Metric: map[string]string{},
							Value:  []interface{}{float64(1609459200), "15.5"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		rate, err := reader.GetRate(context.Background(), "http_requests_total", map[string]string{"app": "test"}, "5m")
		require.NoError(t, err)
		assert.Equal(t, 15.5, rate)
	})
}

func TestVictoriaMetricsReader_GetQuantile(t *testing.T) {
	t.Run("calculates quantile successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query().Get("query")
			assert.Contains(t, query, "quantile_over_time(0.95")
			assert.Contains(t, query, "[5m]")

			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "vector",
					Result: []Result{
						{
							Metric: map[string]string{},
							Value:  []interface{}{float64(1609459200), "123.45"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		p95, err := reader.GetQuantile(context.Background(), 0.95, "response_time", nil, "5m")
		require.NoError(t, err)
		assert.Equal(t, 123.45, p95)
	})
}

func TestVictoriaMetricsReader_GetAverage(t *testing.T) {
	t.Run("calculates average successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query().Get("query")
			assert.Contains(t, query, "avg_over_time(")
			assert.Contains(t, query, "[10m]")

			response := QueryResult{
				Status: "success",
				Data: Data{
					ResultType: "vector",
					Result: []Result{
						{
							Metric: map[string]string{},
							Value:  []interface{}{float64(1609459200), "78.9"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		avg, err := reader.GetAverage(context.Background(), "cpu_usage", map[string]string{"host": "server1"}, "10m")
		require.NoError(t, err)
		assert.Equal(t, 78.9, avg)
	})
}

func TestBuildMetricSelector(t *testing.T) {
	tests := []struct {
		name     string
		metric   string
		labels   map[string]string
		expected string
	}{
		{
			name:     "metric without labels",
			metric:   "up",
			labels:   nil,
			expected: "up",
		},
		{
			name:     "metric with empty labels",
			metric:   "up",
			labels:   map[string]string{},
			expected: "up",
		},
		{
			name:   "metric with single label",
			metric: "http_requests_total",
			labels: map[string]string{"method": "GET"},
			// Note: map iteration order is random, so we check contains
			expected: `http_requests_total{method="GET"}`,
		},
		{
			name:   "metric with multiple labels",
			metric: "http_requests_total",
			labels: map[string]string{
				"method": "GET",
				"status": "200",
			},
			// Can't test exact string due to map iteration, just verify structure
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMetricSelector(tt.metric, tt.labels)

			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			} else {
				// For multiple labels, just verify structure
				assert.Contains(t, result, tt.metric+"{")
				for k, v := range tt.labels {
					assert.Contains(t, result, k+`="`+v+`"`)
				}
				assert.Contains(t, result, "}")
			}
		})
	}
}

func TestVictoriaMetricsReader_Timeout(t *testing.T) {
	t.Run("respects timeout configuration", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			response := QueryResult{Status: "success", Data: Data{}}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 500*time.Millisecond)

		_, err := reader.InstantQuery(context.Background(), "up", time.Time{})
		require.Error(t, err)
		// Should timeout before the server responds
	})

	t.Run("uses default timeout when not specified", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, "localhost:8428", 0)

		assert.Equal(t, 30*time.Second, reader.Timeout)
	})
}

func TestVictoriaMetricsReader_ContextCancellation(t *testing.T) {
	t.Run("respects context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			response := QueryResult{Status: "success", Data: Data{}}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		reader := NewVictoriaMetricsReader(log, strings.TrimPrefix(server.URL, "http://"), 10*time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		_, err := reader.InstantQuery(ctx, "up", time.Time{})
		require.Error(t, err)
	})
}
