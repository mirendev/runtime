package logs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

type mockLogEntry struct {
	Time   string `json:"_time"`
	Msg    string `json:"_msg"`
	Stream string `json:"stream"`
	Source string `json:"source,omitempty"`
}

func createMockVictoriaLogs(t *testing.T, entries []mockLogEntry, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for _, entry := range entries {
			if delay > 0 {
				time.Sleep(delay)
			}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Errorf("failed to marshal entry: %v", err)
				return
			}
			w.Write(data)
			w.Write([]byte("\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

// createFilteringMockVictoriaLogs creates a mock server that filters entries based on query
func createFilteringMockVictoriaLogs(t *testing.T, entries []mockLogEntry) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		t.Logf("Received query: %s", query)

		// Filter entries based on query terms
		var filtered []mockLogEntry
		for _, entry := range entries {
			parts := strings.Split(query, " ")
			shouldInclude := true
			for _, part := range parts[1:] { // Skip the entity/sandbox part
				part = strings.TrimPrefix(part, "*:")
				if unquoted, err := strconv.Unquote(part); err == nil {
					part = unquoted
				}
				if part != "" && !strings.Contains(entry.Msg, part) {
					shouldInclude = false
					break
				}
			}
			if shouldInclude {
				filtered = append(filtered, entry)
			}
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for _, entry := range filtered {
			data, err := json.Marshal(entry)
			if err != nil {
				t.Errorf("failed to marshal entry: %v", err)
				return
			}
			w.Write(data)
			w.Write([]byte("\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

func setupTestServer(t *testing.T, mockServer *httptest.Server) (*Server, *entityserver.Client, func()) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	lr := &observability.LogReader{
		Address: mockServer.URL,
		Timeout: 30 * time.Second,
	}

	server := NewServer(slog.Default(), ec, lr)

	return server, ec, func() {
		cleanup()
		mockServer.Close()
	}
}

func TestStreamLogChunks_Basic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Add(-2 * time.Second).Format(time.RFC3339Nano), Msg: "log line 1", Stream: "stdout"},
		{Time: now.Add(-1 * time.Second).Format(time.RFC3339Nano), Msg: "log line 2", Stream: "stderr"},
		{Time: now.Format(time.RFC3339Nano), Msg: "log line 3", Stream: "stdout", Source: "worker-1"},
	}

	mockServer := createMockVictoriaLogs(t, entries, 0)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	// Create a test app
	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	// Create RPC client
	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	// Collect received chunks
	var receivedChunks []*app_v1alpha.LogChunk
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedChunks = append(receivedChunks, chunk)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	// Should have received at least one chunk
	r.NotEmpty(receivedChunks)

	// Count total entries across all chunks
	var totalEntries int
	for _, chunk := range receivedChunks {
		totalEntries += len(chunk.Entries())
	}
	r.Equal(3, totalEntries)

	// Verify first chunk has expected content
	firstChunk := receivedChunks[0]
	r.NotEmpty(firstChunk.Entries())
	r.Equal("log line 1", firstChunk.Entries()[0].Line())
}

func TestStreamLogChunks_Chunking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r := require.New(t)

	// Create more entries than chunk size (100)
	now := time.Now()
	entries := make([]mockLogEntry, 250)
	for i := range entries {
		entries[i] = mockLogEntry{
			Time:   now.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano),
			Msg:    "log line",
			Stream: "stdout",
		}
	}

	mockServer := createMockVictoriaLogs(t, entries, 0)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	// Create a test app
	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedChunks []*app_v1alpha.LogChunk
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedChunks = append(receivedChunks, chunk)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	// Should have multiple chunks (250 entries / 100 per chunk = 3 chunks)
	r.GreaterOrEqual(len(receivedChunks), 2)

	// First chunks should be full (100 entries)
	r.Equal(100, len(receivedChunks[0].Entries()))
	r.Equal(100, len(receivedChunks[1].Entries()))

	// Last chunk should have remaining entries
	r.Equal(50, len(receivedChunks[2].Entries()))

	// Total should be 250
	var total int
	for _, chunk := range receivedChunks {
		total += len(chunk.Entries())
	}
	r.Equal(250, total)
}

func TestStreamLogChunks_BySandbox(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Format(time.RFC3339Nano), Msg: "sandbox log", Stream: "stdout"},
	}

	mockServer := createMockVictoriaLogs(t, entries, 0)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	// Create the sandbox entity so resolution succeeds
	_, err := ec.Create(ctx, "test-sandbox-123", &compute_v1alpha.Sandbox{})
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedChunks []*app_v1alpha.LogChunk
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedChunks = append(receivedChunks, chunk)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetSandbox("sandbox/test-sandbox-123")

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	r.NotEmpty(receivedChunks)
	r.Equal("sandbox log", receivedChunks[0].Entries()[0].Line())
}

func TestStreamLogChunks_WithFromTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Format(time.RFC3339Nano), Msg: "recent log", Stream: "stdout"},
	}

	mockServer := createMockVictoriaLogs(t, entries, 0)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedChunks []*app_v1alpha.LogChunk
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedChunks = append(receivedChunks, chunk)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	fromTime := standard.ToTimestamp(now.Add(-1 * time.Hour))

	_, err = client.StreamLogChunks(ctx, target, fromTime, false, "", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	r.NotEmpty(receivedChunks)
}

// TestStreamLogChunks_WithUntilTime verifies the `to` bound is threaded all the
// way to the VictoriaLogs query as the `end` parameter, rather than the server
// hardcoding end=now.
func TestStreamLogChunks_WithUntilTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	until := time.Now().Add(-30 * time.Minute)

	var mu sync.Mutex
	var gotEnd string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		gotEnd = req.URL.Query().Get("end")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
	}))

	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error { return nil })

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback, standard.ToTimestamp(until))
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()
	r.NotEmpty(gotEnd, "expected the server to forward an end bound to victorialogs")
	parsed, err := time.Parse(time.RFC3339Nano, gotEnd)
	r.NoError(err)
	r.WithinDuration(until, parsed, time.Second)
}

func TestStreamLogChunks_FollowModePeriodicFlush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := require.New(t)

	// Create a mock server that sends entries slowly (one every 300ms)
	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Format(time.RFC3339Nano), Msg: "log 1", Stream: "stdout"},
		{Time: now.Add(100 * time.Millisecond).Format(time.RFC3339Nano), Msg: "log 2", Stream: "stdout"},
		{Time: now.Add(200 * time.Millisecond).Format(time.RFC3339Nano), Msg: "log 3", Stream: "stdout"},
	}

	// Use /select/logsql/tail endpoint for follow mode
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for i, entry := range entries {
			if i > 0 {
				time.Sleep(300 * time.Millisecond)
			}
			data, _ := json.Marshal(entry)
			w.Write(data)
			w.Write([]byte("\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer mockServer.Close()

	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedChunks []*app_v1alpha.LogChunk
	var chunkTimes []time.Time
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedChunks = append(receivedChunks, chunk)
		chunkTimes = append(chunkTimes, time.Now())
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	_, err = client.StreamLogChunks(ctx, target, nil, true, "", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	// Should have received chunks
	r.NotEmpty(receivedChunks)

	// Total entries should be 3
	var total int
	for _, chunk := range receivedChunks {
		total += len(chunk.Entries())
	}
	r.Equal(3, total)
}

func TestStreamLogChunks_ErrorNoTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	mockServer := createMockVictoriaLogs(t, nil, 0)
	server, _, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		return nil
	})

	// Empty target - should error
	target := &app_v1alpha.LogTarget{}

	_, err := client.StreamLogChunks(ctx, target, nil, false, "", callback, nil)
	r.Error(err)
	r.Contains(err.Error(), "target must specify exactly one of app, sandbox, or system")
}

func TestStreamLogChunks_AppNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	mockServer := createMockVictoriaLogs(t, nil, 0)
	server, _, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("nonexistent-app")

	_, err := client.StreamLogChunks(ctx, target, nil, false, "", callback, nil)
	r.Error(err)
}

func TestStreamLogChunks_LogEntryFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{
			Time:   now.Format(time.RFC3339Nano),
			Msg:    "test message",
			Stream: "stderr",
			Source: "my-source",
		},
	}

	mockServer := createMockVictoriaLogs(t, entries, 0)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedEntry *app_v1alpha.LogEntry
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		if len(chunk.Entries()) > 0 {
			receivedEntry = chunk.Entries()[0]
		}
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	r.NotNil(receivedEntry)
	r.Equal("test message", receivedEntry.Line())
	r.Equal("stderr", receivedEntry.Stream())
	r.Equal("my-source", receivedEntry.Source())
	r.True(receivedEntry.HasTimestamp())
}

func TestStreamLogChunks_FilterPassedToVictoriaLogs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Add(-3 * time.Second).Format(time.RFC3339Nano), Msg: "INFO starting server", Stream: "stdout"},
		{Time: now.Add(-2 * time.Second).Format(time.RFC3339Nano), Msg: "ERROR connection failed", Stream: "stderr"},
		{Time: now.Add(-1 * time.Second).Format(time.RFC3339Nano), Msg: "INFO retrying connection", Stream: "stdout"},
		{Time: now.Format(time.RFC3339Nano), Msg: "ERROR timeout exceeded", Stream: "stderr"},
	}

	// Use filtering mock that simulates VictoriaLogs LogsQL filtering
	mockServer := createFilteringMockVictoriaLogs(t, entries)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedEntries []*app_v1alpha.LogEntry
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedEntries = append(receivedEntries, chunk.Entries()...)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	// Filter for ERROR logs - this should be passed to VictoriaLogs
	_, err = client.StreamLogChunks(ctx, target, nil, false, "ERROR", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	// Mock server simulates filtering, should only return ERROR entries
	r.Len(receivedEntries, 2)
	r.Contains(receivedEntries[0].Line(), "ERROR")
	r.Contains(receivedEntries[1].Line(), "ERROR")
}

func TestStreamLogChunks_FilterNoMatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Add(-2 * time.Second).Format(time.RFC3339Nano), Msg: "INFO normal operation", Stream: "stdout"},
		{Time: now.Add(-1 * time.Second).Format(time.RFC3339Nano), Msg: "DEBUG checking status", Stream: "stdout"},
	}

	mockServer := createFilteringMockVictoriaLogs(t, entries)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedEntries []*app_v1alpha.LogEntry
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedEntries = append(receivedEntries, chunk.Entries()...)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	// Filter for something that doesn't exist
	_, err = client.StreamLogChunks(ctx, target, nil, false, "CRITICAL", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	// Should have no entries
	r.Empty(receivedEntries)
}

func TestStreamLogChunks_FilterWithFollow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Format(time.RFC3339Nano), Msg: "INFO normal log", Stream: "stdout"},
		{Time: now.Add(100 * time.Millisecond).Format(time.RFC3339Nano), Msg: "ERROR problem found", Stream: "stderr"},
		{Time: now.Add(200 * time.Millisecond).Format(time.RFC3339Nano), Msg: "INFO continuing", Stream: "stdout"},
	}

	// Create a mock server that filters and streams entries slowly
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		t.Logf("Received query: %s", query)

		// Strip pipe operators (like "| sort by (_time)")
		if pipeIdx := strings.Index(query, " |"); pipeIdx != -1 {
			query = query[:pipeIdx]
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for i, entry := range entries {
			if i > 0 {
				time.Sleep(100 * time.Millisecond)
			}

			// Simple filter simulation
			parts := strings.Split(query, " ")
			shouldInclude := true
			for _, part := range parts[1:] {
				part = strings.TrimPrefix(part, "*:")
				if unquoted, err := strconv.Unquote(part); err == nil {
					part = unquoted
				}
				if part != "" && !strings.Contains(entry.Msg, part) {
					shouldInclude = false
					break
				}
			}

			if shouldInclude {
				data, _ := json.Marshal(entry)
				w.Write(data)
				w.Write([]byte("\n"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}))
	defer mockServer.Close()

	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	var receivedEntries []*app_v1alpha.LogEntry
	var mu sync.Mutex

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		mu.Lock()
		defer mu.Unlock()
		receivedEntries = append(receivedEntries, chunk.Entries()...)
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	// Filter for ERROR in follow mode
	_, err = client.StreamLogChunks(ctx, target, nil, true, "ERROR", callback, nil)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	// Should only have the ERROR entry
	r.Len(receivedEntries, 1)
	r.Contains(receivedEntries[0].Line(), "ERROR")
}

func TestLogTarget_QueryWithFilter(t *testing.T) {
	// Test that LogTarget.query() correctly appends the filter
	target := observability.LogTarget{
		EntityID: "app/test-app",
		Filter:   "ERROR",
	}

	query := target.Query()
	require.Contains(t, query, "entity:")
	require.Contains(t, query, "ERROR")
}

func TestLogTarget_QueryWithoutFilter(t *testing.T) {
	// Test that LogTarget.query() works without filter
	target := observability.LogTarget{
		SandboxID: "sandbox/test",
	}

	query := target.Query()
	require.Contains(t, query, "sandbox:")

	// With a filter, the query should include both sandbox selector and filter
	targetWithFilter := observability.LogTarget{
		SandboxID: "sandbox/test",
		Filter:    "error",
	}
	filteredQuery := targetWithFilter.Query()
	require.Contains(t, filteredQuery, "sandbox:")
	require.Contains(t, filteredQuery, "sandbox/test")
	require.Contains(t, filteredQuery, "error")
}

func TestStreamLogChunks_InvalidFilterRegex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	mockServer := createMockVictoriaLogs(t, nil, 0)
	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	// Invalid regex should return error
	_, err = client.StreamLogChunks(ctx, target, nil, false, "/[invalid/", callback, nil)
	r.Error(err)
	r.Contains(err.Error(), "invalid filter")
}

func TestCompileLogFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "app word", input: `-source:"system" error`, want: `-source:"system" *:"error"`},
		{name: "sandbox phrase", input: `"connection failed"`, want: `*:"connection failed"`},
		{name: "service and multiple terms", input: `(service:"web" OR miren.service:"web") error timeout`, want: `(service:"web" OR miren.service:"web") *:"error" *:"timeout"`},
		{name: "build regex", input: `source:build version:"v3" /fail.*/`, want: `source:build version:"v3" *:~"fail.*"`},
		{name: "run negation", input: `(run:"run/abc" OR miren.run:"run/abc") -debug`, want: `(run:"run/abc" OR miren.run:"run/abc") -(*:"debug")`},
		{name: "system phrase", input: `source:"system" module:"scheduler" "lease expired"`, want: `source:"system" module:"scheduler" *:"lease expired"`},
		{name: "near build prefix remains grep", input: `-source:"system" source:builder`, want: `-source:"system" *:"source:builder"`},
		{name: "malformed service selector remains grep", input: `(service:"web") error`, want: `*:"(service:\"web\")" *:"error"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compileLogFilter(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCompileSeparatedFilter(t *testing.T) {
	tests := []struct {
		name       string
		structural string
		grep       string
		want       string
		wantErr    bool
	}{
		{name: "service selector and punctuation", structural: `(service:"web" OR miren.service:"web")`, grep: `status=500`, want: `(service:"web" OR miren.service:"web") *:"status=500"`},
		{name: "build selector", structural: `source:build version:"v3"`, grep: `error`, want: `source:build version:"v3" *:"error"`},
		{name: "run selector", structural: `(run:"run/abc" OR miren.run:"run/abc")`, grep: `timeout`, want: `(run:"run/abc" OR miren.run:"run/abc") *:"timeout"`},
		{name: "system selector", structural: `source:"system" module:"scheduler"`, grep: `error`, want: `source:"system" module:"scheduler" *:"error"`},
		{name: "structural-looking grep remains literal", structural: `-source:"system"`, grep: `module:"scheduler"`, want: `-source:"system" *:"module:\"scheduler\""`},
		{name: "grep only", grep: `source:builder`, want: `*:"source:builder"`},
		{name: "operator rejected", structural: `OR *`, wantErr: true},
		{name: "pipe rejected", structural: `-source:"system" | fields _msg`, wantErr: true},
		{name: "trailing unknown clause rejected", structural: `source:build arbitrary:true`, wantErr: true},
		{name: "service escaped-quote injection rejected", structural: `(service:"safe\\") OR * # " OR miren.service:"x")`, wantErr: true},
		{name: "run escaped-quote injection rejected", structural: `(run:"safe\\") OR * # " OR miren.run:"x")`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compileSeparatedFilter(tt.structural, tt.grep)
			if tt.wantErr {
				require.ErrorContains(t, err, "invalid structural filter")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStreamLogChunks_FilterWithRegex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Add(-3 * time.Second).Format(time.RFC3339Nano), Msg: "INFO starting server", Stream: "stdout"},
		{Time: now.Add(-2 * time.Second).Format(time.RFC3339Nano), Msg: "ERROR connection failed", Stream: "stderr"},
		{Time: now.Add(-1 * time.Second).Format(time.RFC3339Nano), Msg: "WARN timeout approaching", Stream: "stdout"},
	}

	// Verify that regex filter gets compiled to LogsQL format
	var capturedQuery string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("query")
		t.Logf("Captured query: %s", capturedQuery)

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for _, entry := range entries {
			data, _ := json.Marshal(entry)
			w.Write(data)
			w.Write([]byte("\n"))
		}
	}))
	defer mockServer.Close()

	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	// Use regex filter syntax /pattern/
	_, err = client.StreamLogChunks(ctx, target, nil, false, "/ERR(OR)?/", callback, nil)
	r.NoError(err)

	// Verify the query contains the compiled LogsQL regex format
	r.Contains(capturedQuery, `*:~"ERR(OR)?"`)
}

func TestStreamLogChunks_FilterWithNegation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r := require.New(t)

	entries := []mockLogEntry{
		{Time: time.Now().Format(time.RFC3339Nano), Msg: "test log", Stream: "stdout"},
	}

	// Verify that negation filter gets compiled correctly
	var capturedQuery string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("query")
		t.Logf("Captured query: %s", capturedQuery)

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for _, entry := range entries {
			data, _ := json.Marshal(entry)
			w.Write(data)
			w.Write([]byte("\n"))
		}
	}))
	defer mockServer.Close()

	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		return nil
	})

	target := &app_v1alpha.LogTarget{}
	target.SetApp("test-app")

	// Use negation filter syntax: show errors but not debug
	_, err = client.StreamLogChunks(ctx, target, nil, false, "error -debug", callback, nil)
	r.NoError(err)

	// Verify the query contains negation
	r.Contains(capturedQuery, `*:"error"`)
	r.Contains(capturedQuery, `-(*:"debug")`)
}

func TestStreamLogChunks_QueryContract(t *testing.T) {
	// Verifies that queries sent to VictoriaLogs never contain sort pipes,
	// and use the limit query parameter for bounded queries. Sort pipes
	// force VictoriaLogs to buffer and sort all matching entries server-side,
	// which is extremely slow on large datasets.

	r := require.New(t)

	now := time.Now()
	entries := []mockLogEntry{
		{Time: now.Add(-2 * time.Second).Format(time.RFC3339Nano), Msg: "line 1", Stream: "stdout"},
		{Time: now.Add(-1 * time.Second).Format(time.RFC3339Nano), Msg: "line 2", Stream: "stdout"},
		{Time: now.Format(time.RFC3339Nano), Msg: "line 3", Stream: "stdout"},
	}

	var capturedQueries []string
	var capturedLimits []string
	var mu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedQueries = append(capturedQueries, r.URL.Query().Get("query"))
		capturedLimits = append(capturedLimits, r.URL.Query().Get("limit"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for _, entry := range entries {
			data, _ := json.Marshal(entry)
			w.Write(data)
			w.Write([]byte("\n"))
		}
	}))
	defer mockServer.Close()

	server, ec, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

	app := &core_v1alpha.App{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := ec.Create(ctx, "test-app", app)
	r.NoError(err)

	client := &app_v1alpha.LogsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptLogs(server)),
	}

	noop := stream.Callback(func(chunk *app_v1alpha.LogChunk) error { return nil })

	t.Run("default query uses limit param not sort pipe", func(t *testing.T) {
		mu.Lock()
		capturedQueries = nil
		capturedLimits = nil
		mu.Unlock()

		target := &app_v1alpha.LogTarget{}
		target.SetApp("test-app")

		// No from time, no follow → server applies default limit
		_, err := client.StreamLogChunks(ctx, target, nil, false, "", noop, nil)
		r.NoError(err)

		mu.Lock()
		defer mu.Unlock()
		r.NotEmpty(capturedQueries)
		r.NotContains(capturedQueries[0], "sort", "query must not contain sort pipe")
		r.NotContains(capturedQueries[0], "|", "query must not contain pipe operators")
		r.Equal("100", capturedLimits[0], "default query should set limit=100")
	})

	t.Run("time-bounded query has no limit param", func(t *testing.T) {
		mu.Lock()
		capturedQueries = nil
		capturedLimits = nil
		mu.Unlock()

		target := &app_v1alpha.LogTarget{}
		target.SetApp("test-app")

		fromTime := standard.ToTimestamp(now.Add(-5 * time.Minute))
		_, err := client.StreamLogChunks(ctx, target, fromTime, false, "", noop, nil)
		r.NoError(err)

		mu.Lock()
		defer mu.Unlock()
		r.NotEmpty(capturedQueries)
		r.NotContains(capturedQueries[0], "sort", "query must not contain sort pipe")
		r.Empty(capturedLimits[0], "time-bounded query should not set limit param")
	})
}
