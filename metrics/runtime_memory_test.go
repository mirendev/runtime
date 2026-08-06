package metrics

import (
	"context"
	"io"
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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRuntimeMemory_Collect(t *testing.T) {
	var receivedData string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedData = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := NewVictoriaMetricsWriter(testLogger(), strings.TrimPrefix(server.URL, "http://"), 10*time.Second)
	rm := NewRuntimeMemory(testLogger(), writer)

	require.NoError(t, rm.collect(context.Background()))
	writer.flush()

	// These series must always be emitted regardless of the underlying
	// runtime/metrics availability — they map to core, always-present samples.
	expected := []string{
		"go_mem_heap_alloc_bytes",
		"go_mem_heap_inuse_bytes",
		"go_mem_heap_sys_bytes",
		"go_mem_heap_idle_bytes",
		"go_mem_heap_released_bytes",
		"go_mem_heap_objects",
		"go_mem_stack_inuse_bytes",
		"go_mem_sys_bytes",
		"go_mem_next_gc_bytes",
		"go_gc_count_total",
		"go_goroutines",
	}
	for _, name := range expected {
		assert.Contains(t, receivedData, name+"{", "expected series %q", name)
	}

	// Every series carries the control-process entity label.
	for line := range strings.SplitSeq(strings.TrimSpace(receivedData), "\n") {
		if line == "" {
			continue
		}
		assert.Contains(t, line, `entity="miren/control"`, "line missing entity label: %s", line)
	}
}

func TestRuntimeMemory_MonitorNilWriterIsNoop(t *testing.T) {
	rm := NewRuntimeMemory(testLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// With a nil Writer, Monitor must return immediately rather than block on
	// its ticker, so this completes well within the timeout.
	done := make(chan struct{})
	go func() {
		rm.Monitor(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Monitor did not return immediately with a nil Writer")
	}
}
