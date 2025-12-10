package logs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
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

		// Strip pipe operators (like "| sort by (_time)")
		if pipeIdx := strings.Index(query, " |"); pipeIdx != -1 {
			query = query[:pipeIdx]
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		for _, entry := range entries {
			// Simple filter simulation: if query contains a word filter, check if msg contains it
			// This simulates VictoriaLogs LogsQL word filtering
			parts := strings.Split(query, " ")
			shouldInclude := true
			for _, part := range parts[1:] { // Skip the entity/sandbox part
				if part != "" && !strings.Contains(entry.Msg, part) {
					shouldInclude = false
					break
				}
			}

			if shouldInclude {
				data, err := json.Marshal(entry)
				if err != nil {
					t.Errorf("failed to marshal entry: %v", err)
					return
				}
				w.Write(data)
				w.Write([]byte("\n"))
			}
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

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback)
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

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback)
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
	server, _, cleanup := setupTestServer(t, mockServer)
	defer cleanup()

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

	_, err := client.StreamLogChunks(ctx, target, nil, false, "", callback)
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

	_, err = client.StreamLogChunks(ctx, target, fromTime, false, "", callback)
	r.NoError(err)

	mu.Lock()
	defer mu.Unlock()

	r.NotEmpty(receivedChunks)
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

	_, err = client.StreamLogChunks(ctx, target, nil, true, "", callback)
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

	_, err := client.StreamLogChunks(ctx, target, nil, false, "", callback)
	r.Error(err)
	r.Contains(err.Error(), "target must specify either app or sandbox")
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

	_, err := client.StreamLogChunks(ctx, target, nil, false, "", callback)
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

	_, err = client.StreamLogChunks(ctx, target, nil, false, "", callback)
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
	_, err = client.StreamLogChunks(ctx, target, nil, false, "ERROR", callback)
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
	_, err = client.StreamLogChunks(ctx, target, nil, false, "CRITICAL", callback)
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
	_, err = client.StreamLogChunks(ctx, target, nil, true, "ERROR", callback)
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
	require.NotContains(t, query, " ") // No filter appended
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
	_, err = client.StreamLogChunks(ctx, target, nil, false, "/[invalid/", callback)
	r.Error(err)
	r.Contains(err.Error(), "invalid filter")
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
	_, err = client.StreamLogChunks(ctx, target, nil, false, "/ERR(OR)?/", callback)
	r.NoError(err)

	// Verify the query contains the compiled LogsQL regex format
	r.Contains(capturedQuery, `~"ERR(OR)?"`)
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
	_, err = client.StreamLogChunks(ctx, target, nil, false, "error -debug", callback)
	r.NoError(err)

	// Verify the query contains negation
	r.Contains(capturedQuery, "error")
	r.Contains(capturedQuery, "-debug")
}
