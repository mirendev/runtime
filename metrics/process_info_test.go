package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessInfo_Collect(t *testing.T) {
	var receivedData string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedData = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := NewVictoriaMetricsWriter(testLogger(), strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
	pi := NewProcessInfo(testLogger(), writer)
	pi.StartTime = time.Unix(1_700_000_000, 0)
	pi.Version = "main:abc1234"
	pi.Commit = "abc1234def"
	pi.Channel = "main"

	require.NoError(t, pi.collect(context.Background()))
	writer.flush()

	lines := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(receivedData), "\n") {
		name, _, _ := strings.Cut(line, "{")
		lines[name] = line
		assert.Contains(t, line, `entity="miren/control"`, "line missing entity label: %s", line)
	}

	start, ok := lines["process_start_time_seconds"]
	require.True(t, ok, "expected process_start_time_seconds in %q", receivedData)
	assert.Contains(t, start, "} 1700000000 ", "start time is the configured StartTime in unix seconds")

	build, ok := lines["miren_build_info"]
	require.True(t, ok, "expected miren_build_info in %q", receivedData)
	assert.Contains(t, build, `version="main:abc1234"`)
	assert.Contains(t, build, `commit="abc1234def"`)
	assert.Contains(t, build, `channel="main"`)
	assert.Contains(t, build, "} 1 ", "build_info is an always-1 gauge")
}

func TestProcessInfo_OmitsEmptyChannel(t *testing.T) {
	var receivedData string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedData = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := NewVictoriaMetricsWriter(testLogger(), strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
	pi := NewProcessInfo(testLogger(), writer)
	pi.Channel = ""

	require.NoError(t, pi.collect(context.Background()))
	writer.flush()

	assert.Contains(t, receivedData, "miren_build_info{")
	assert.NotContains(t, receivedData, "channel=")
}

func TestProcessInfo_DefaultsDescribeThisProcess(t *testing.T) {
	pi := NewProcessInfo(testLogger(), nil)

	assert.Equal(t, "miren/control", pi.Entity)
	assert.False(t, pi.StartTime.IsZero())
	assert.False(t, pi.StartTime.After(time.Now()), "start time must not be in the future")
	// Untagged test binaries carry the ldflags zero values, and those must
	// travel as-is rather than be blanked.
	assert.NotEmpty(t, pi.Version)
	assert.NotEmpty(t, pi.Commit)
}

func TestProcessInfo_MonitorNilWriterIsNoop(t *testing.T) {
	pi := NewProcessInfo(testLogger(), nil)

	done := make(chan struct{})
	go func() {
		pi.Monitor(t.Context())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Monitor did not return immediately with a nil Writer")
	}
}
