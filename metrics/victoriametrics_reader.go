package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// VictoriaMetricsReader reads metrics from VictoriaMetrics using MetricsQL
type VictoriaMetricsReader struct {
	Log     *slog.Logger
	Address string
	Timeout time.Duration
	client  *http.Client
}

// NewVictoriaMetricsReader creates a new VictoriaMetrics reader
func NewVictoriaMetricsReader(log *slog.Logger, address string, timeout time.Duration) *VictoriaMetricsReader {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &VictoriaMetricsReader{
		Log:     log,
		Address: address,
		Timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// QueryResult represents the result of a MetricsQL query
type QueryResult struct {
	Status string `json:"status"`
	Data   Data   `json:"data"`
}

type Data struct {
	ResultType string   `json:"resultType"`
	Result     []Result `json:"result"`
}

type Result struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value,omitempty"`  // For instant queries: [timestamp, value_string]
	Values [][]interface{}   `json:"values,omitempty"` // For range queries: [[timestamp, value_string], ...]
}

// InstantQuery executes an instant MetricsQL query (point-in-time)
func (r *VictoriaMetricsReader) InstantQuery(ctx context.Context, query string, ts time.Time) (*QueryResult, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("latency_offset", "2s")
	if !ts.IsZero() {
		params.Set("time", strconv.FormatInt(ts.Unix(), 10))
	}

	queryURL := fmt.Sprintf("http://%s/api/v1/query?%s", r.Address, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("victoriametrics returned status %d: %s", resp.StatusCode, string(body))
	}

	var result QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", result.Status)
	}

	return &result, nil
}

// RangeQuery executes a range MetricsQL query (time series)
func (r *VictoriaMetricsReader) RangeQuery(ctx context.Context, query string, start, end time.Time, step string) (*QueryResult, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("latency_offset", "2s")
	if step != "" {
		params.Set("step", step)
	}

	queryURL := fmt.Sprintf("http://%s/api/v1/query_range?%s", r.Address, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("victoriametrics returned status %d: %s", resp.StatusCode, string(body))
	}

	var result QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", result.Status)
	}

	return &result, nil
}

// Helper methods for common query patterns

// GetLatestValue retrieves the latest value for a metric
func (r *VictoriaMetricsReader) GetLatestValue(ctx context.Context, metricName string, labels map[string]string) (float64, error) {
	query := buildMetricSelector(metricName, labels)
	result, err := r.InstantQuery(ctx, query, time.Time{})
	if err != nil {
		return 0, err
	}

	if len(result.Data.Result) == 0 {
		return 0, fmt.Errorf("no data found for metric %s", metricName)
	}

	valueStr, ok := result.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type in response")
	}

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse value: %w", err)
	}

	return value, nil
}

// GetTimeSeries retrieves time series data for a metric over a time range
func (r *VictoriaMetricsReader) GetTimeSeries(ctx context.Context, metricName string, labels map[string]string, start, end time.Time, step string) ([]TimeSeriesPoint, error) {
	query := buildMetricSelector(metricName, labels)
	result, err := r.RangeQuery(ctx, query, start, end, step)
	if err != nil {
		return nil, err
	}

	if len(result.Data.Result) == 0 {
		return []TimeSeriesPoint{}, nil
	}

	var points []TimeSeriesPoint
	for _, value := range result.Data.Result[0].Values {
		timestamp, ok := value[0].(float64)
		if !ok {
			continue
		}

		valueStr, ok := value[1].(string)
		if !ok {
			continue
		}

		floatValue, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}

		points = append(points, TimeSeriesPoint{
			Timestamp: time.Unix(int64(timestamp), 0),
			Value:     floatValue,
		})
	}

	return points, nil
}

// GetRate calculates the rate of a counter metric over a time range
func (r *VictoriaMetricsReader) GetRate(ctx context.Context, metricName string, labels map[string]string, window string) (float64, error) {
	selector := buildMetricSelector(metricName, labels)
	query := fmt.Sprintf("rate(%s[%s])", selector, window)
	return r.GetLatestValue(ctx, query, nil)
}

// GetQuantile calculates a quantile over time
func (r *VictoriaMetricsReader) GetQuantile(ctx context.Context, quantile float64, metricName string, labels map[string]string, window string) (float64, error) {
	selector := buildMetricSelector(metricName, labels)
	query := fmt.Sprintf("quantile_over_time(%f, %s[%s])", quantile, selector, window)
	return r.GetLatestValue(ctx, query, nil)
}

// GetAverage calculates the average value over time
func (r *VictoriaMetricsReader) GetAverage(ctx context.Context, metricName string, labels map[string]string, window string) (float64, error) {
	selector := buildMetricSelector(metricName, labels)
	query := fmt.Sprintf("avg_over_time(%s[%s])", selector, window)
	return r.GetLatestValue(ctx, query, nil)
}

// TimeSeriesPoint represents a single point in a time series
type TimeSeriesPoint struct {
	Timestamp time.Time
	Value     float64
}

// buildMetricSelector builds a MetricsQL metric selector from name and labels
func buildMetricSelector(metricName string, labels map[string]string) string {
	if len(labels) == 0 {
		return metricName
	}

	selector := metricName + "{"
	first := true
	for k, v := range labels {
		if !first {
			selector += ","
		}
		first = false
		selector += fmt.Sprintf(`%s="%s"`, k, v)
	}
	selector += "}"

	return selector
}
