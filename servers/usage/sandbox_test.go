package usage

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/metrics"
)

// metricStub answers each instant query with the rows registered for the
// metric it reads, so memory and swap can carry different values and a query
// that read the wrong series would show it.
func metricStub(t *testing.T, byMetric map[string][]metrics.Result) *metrics.VictoriaMetricsReader {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		require.NoError(t, err)

		var rows []metrics.Result
		for metric, rs := range byMetric {
			if strings.Contains(q.Get("query"), metric) {
				rows = rs
			}
		}

		body, err := json.Marshal(metrics.QueryResult{
			Status: "success",
			Data:   metrics.Data{ResultType: "vector", Result: rows},
		})
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return metrics.NewVictoriaMetricsReader(slog.New(slog.DiscardHandler), srv.URL, 5*time.Second)
}

func sandboxValue(sandbox, value string) metrics.Result {
	return metrics.Result{
		Metric: map[string]string{labelSandbox: sandbox},
		Value:  []any{float64(100), value},
	}
}

// Swap is read from its own series and reported per sandbox. A sandbox whose
// runner has not written that series yet is left out, so the row reports swap
// as unmeasured instead of claiming the sandbox uses none.
func TestSandboxSamplesReportSwapSeparatelyFromMemory(t *testing.T) {
	s := &Server{Reader: metricStub(t, map[string][]metrics.Result{
		metricMemoryUsed: {sandboxValue("sandbox/a", "1000"), sandboxValue("sandbox/old", "3000")},
		metricSwapUsed:   {sandboxValue("sandbox/a", "250")},
	})}
	w := window{start: time.Unix(40, 0), end: time.Unix(100, 0), aggregate: aggregateAvg}

	_, memory, swap, _, warnings := s.sandboxSamples(context.Background(), filter{}, w, &directory{})
	require.Empty(t, warnings)

	assert.Equal(t, 1000.0, memory["sandbox/a"])
	assert.Equal(t, 250.0, swap["sandbox/a"], "swap must come from swap_usage_bytes, not resident memory")

	_, measured := swap["sandbox/old"]
	assert.False(t, measured, "a sandbox with no swap series must read as unmeasured, not zero")
}

func TestSingleSandboxSwapIsNilWhenUnmeasured(t *testing.T) {
	w := window{start: time.Unix(40, 0), end: time.Unix(100, 0), aggregate: aggregateAvg}

	reporting := &Server{Reader: metricStub(t, map[string][]metrics.Result{
		metricMemoryUsed: {sandboxValue("sandbox/a", "1000")},
		metricSwapUsed:   {sandboxValue("sandbox/a", "0")},
	})}
	_, bytes, swap, warnings := reporting.singleSandboxSamples(context.Background(), "", w)
	require.Empty(t, warnings)
	assert.Equal(t, 1000.0, bytes)
	require.NotNil(t, swap, "a measured zero is still a measurement")
	assert.Equal(t, 0.0, *swap)

	old := &Server{Reader: metricStub(t, map[string][]metrics.Result{
		metricMemoryUsed: {sandboxValue("sandbox/a", "1000")},
	})}
	_, _, swap, _ = old.singleSandboxSamples(context.Background(), "", w)
	assert.Nil(t, swap)
}
