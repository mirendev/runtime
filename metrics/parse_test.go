package metrics

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The parser only has to understand what the writer produces, so the test that
// matters is the round trip: whatever the writer formats comes back unchanged.
func TestParsePointsRoundTripsWriterOutput(t *testing.T) {
	w := NewVictoriaMetricsWriter(nil, "unused", 0)
	ts := time.UnixMilli(1759170000123)

	points := []MetricPoint{
		{Name: "go_goroutines", Labels: map[string]string{"entity": "miren/runner", "miren_runner": "r1"}, Value: 42, Timestamp: ts},
		{Name: "node_load1", Labels: map[string]string{"miren_node": "node/abc"}, Value: 0.125, Timestamp: ts},
		{Name: "bare_metric", Value: -3.5, Timestamp: ts},
		{Name: "tricky", Labels: map[string]string{"v": "quote\" back\\slash\nnewline,comma}brace"}, Value: 1, Timestamp: ts},
		{Name: "big", Labels: map[string]string{"a": ""}, Value: 1.759170000e9, Timestamp: ts},
	}

	var lines []string
	for _, p := range points {
		lines = append(lines, w.formatMetricLine(p))
	}

	result, err := ParsePoints([]byte(strings.Join(lines, "\n")+"\n"), 0)
	require.NoError(t, err)
	require.Zero(t, result.Invalid)
	require.Zero(t, result.OverLimit)
	got := result.Points
	require.Len(t, got, len(points))
	for i := range points {
		assert.Equal(t, points[i].Name, got[i].Name)
		assert.Equal(t, points[i].Value, got[i].Value)
		assert.True(t, points[i].Timestamp.Equal(got[i].Timestamp), "timestamp %d", i)
		if len(points[i].Labels) == 0 {
			assert.Empty(t, got[i].Labels)
		} else {
			assert.Equal(t, points[i].Labels, got[i].Labels)
		}
	}
}

func TestParsePointsAcceptsMissingTimestampAndSpecialValues(t *testing.T) {
	before := time.Now()
	result, err := ParsePoints([]byte("# a comment\n\nup 1\nnan_metric NaN 1000\ninf_metric +Inf 1000\n"), 0)
	require.NoError(t, err)
	got := result.Points
	require.Len(t, got, 3)

	assert.False(t, got[0].Timestamp.Before(before), "untimestamped sample is stamped at parse time")
	assert.True(t, math.IsNaN(got[1].Value))
	assert.True(t, math.IsInf(got[2].Value, 1))
}

// A bad line costs that one sample, not the batch around it.
func TestParsePointsSkipsMalformedLines(t *testing.T) {
	for _, line := range []string{
		"{a=\"b\"} 1",
		"m{a=b} 1",
		"m{a=\"b} 1",
		"m{a=\"b\" 1",
		"m{a=\"\\q\"} 1",
		"m notanumber",
		"m 1 notatimestamp",
		"m 1 2 3",
		"m",
		"m.dotted 1",
		"1m 1",
		"m{miren.cluster=\"x\"} 1",
		"m{0a=\"x\"} 1",
		"m{a:b=\"x\"} 1",
	} {
		result, err := ParsePoints([]byte("good 1 1\n"+line+"\nalso_good 2 1\n"), 0)
		require.NoError(t, err)
		assert.Equal(t, 1, result.Invalid, "%q", line)
		assert.ErrorContains(t, result.FirstInvalid, "line 2", "%q", line)
		require.Len(t, result.Points, 2, "%q", line)
		assert.Equal(t, "also_good", result.Points[1].Name)
	}
}

// Past the limit, samples are counted but not parsed or kept, so a huge batch
// costs the caller a scan rather than an allocation per sample.
func TestParsePointsStopsAtLimit(t *testing.T) {
	result, err := ParsePoints([]byte("a 1 1\n# comment\nb 2 1\nc 3 1\nd{broken 4\n"), 2)
	require.NoError(t, err)
	require.Len(t, result.Points, 2)
	assert.Equal(t, "b", result.Points[1].Name)
	assert.Equal(t, 2, result.OverLimit)
	assert.Zero(t, result.Invalid, "samples past the limit are not parsed, so a bad one there is not counted as invalid")
}
