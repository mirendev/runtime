package observability

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBatchLogWriter_FlushOnTimer(t *testing.T) {
	var mu sync.Mutex
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plw := NewPersistentLogWriter(srv.URL, 5*time.Second)
	bw := NewBatchLogWriter(plw)

	// Write a single entry — below the count threshold
	err := bw.WriteEntry("test-entity", LogEntry{
		Timestamp: time.Now(),
		Stream:    Stdout,
		Body:      "hello timer",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the timer to flush (defaultFlushInterval is 250ms)
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one flush from timer, got none")
	}

	// Verify the NDJSON content
	lines := strings.Split(strings.TrimSpace(received[0]), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var logData map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &logData); err != nil {
		t.Fatal(err)
	}
	if logData["_msg"] != "hello timer" {
		t.Fatalf("expected _msg=hello timer, got %v", logData["_msg"])
	}
	if logData["entity"] != "test-entity" {
		t.Fatalf("expected entity=test-entity, got %v", logData["entity"])
	}

	bw.Close()
}

func TestBatchLogWriter_FlushOnThreshold(t *testing.T) {
	var mu sync.Mutex
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plw := NewPersistentLogWriter(srv.URL, 5*time.Second)
	bw := NewBatchLogWriter(plw)

	// Write exactly defaultFlushCount entries to trigger threshold flush
	for i := range defaultFlushCount {
		err := bw.WriteEntry("test-entity", LogEntry{
			Timestamp: time.Now(),
			Stream:    Stdout,
			Body:      "entry " + time.Now().Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Give the background goroutine a moment to process
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected threshold flush, got no POSTs")
	}

	// The first POST should contain at least defaultFlushCount lines
	mu.Lock()
	lines := strings.Split(strings.TrimSpace(received[0]), "\n")
	mu.Unlock()
	if len(lines) < defaultFlushCount {
		t.Fatalf("expected at least %d lines in first POST, got %d", defaultFlushCount, len(lines))
	}

	bw.Close()
}

func TestBatchLogWriter_ReservedFieldsProtected(t *testing.T) {
	var mu sync.Mutex
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plw := NewPersistentLogWriter(srv.URL, 5*time.Second)
	bw := NewBatchLogWriter(plw)

	// Write an entry with attributes that collide with reserved field names
	err := bw.WriteEntry("correct-entity", LogEntry{
		Timestamp: time.Now(),
		Stream:    Stdout,
		Body:      "test message",
		Attributes: map[string]string{
			"entity": "wrong-entity",
			"stream": "wrong-stream",
			"source": "my-source",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	bw.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected flush, got none")
	}

	var logData map[string]any
	line := strings.TrimSpace(received[0])
	if err := json.Unmarshal([]byte(line), &logData); err != nil {
		t.Fatal(err)
	}

	// Reserved fields must not be overwritten by attributes
	if logData["entity"] != "correct-entity" {
		t.Errorf("entity = %v, want %q (attribute overwrote reserved field)", logData["entity"], "correct-entity")
	}
	if logData["stream"] != "stdout" {
		t.Errorf("stream = %v, want %q (attribute overwrote reserved field)", logData["stream"], "stdout")
	}
	// Non-reserved attributes should still be written
	if logData["source"] != "my-source" {
		t.Errorf("source = %v, want %q (non-reserved attribute was dropped)", logData["source"], "my-source")
	}
}

func TestParseLogLine_FiltersInternalFields(t *testing.T) {
	lr := &LogReader{}

	input := `{"_msg":"hello","_time":"2026-03-13T16:30:00Z","stream":"stdout","entity":"app/test","trace_id":"abc","_stream":"{}","_stream_id":"0000e934a84adb05","source":"system","module":"etcd","service":"web"}`

	entry, err := lr.parseLogLine([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	// Standard fields parsed correctly
	if entry.Body != "hello" {
		t.Errorf("Body = %q, want %q", entry.Body, "hello")
	}
	if entry.Stream != Stdout {
		t.Errorf("Stream = %q, want %q", entry.Stream, Stdout)
	}
	if entry.TraceID != "abc" {
		t.Errorf("TraceID = %q, want %q", entry.TraceID, "abc")
	}

	// VictoriaLogs internal fields must not appear as attributes
	for _, key := range []string{"_stream", "_stream_id", "_msg", "_time"} {
		if _, ok := entry.Attributes[key]; ok {
			t.Errorf("internal field %q leaked into attributes", key)
		}
	}

	// Reserved routing fields must not appear as attributes
	for _, key := range []string{"entity", "stream", "trace_id"} {
		if _, ok := entry.Attributes[key]; ok {
			t.Errorf("reserved field %q leaked into attributes", key)
		}
	}

	// User attributes must be preserved
	for _, key := range []string{"source", "module", "service"} {
		if _, ok := entry.Attributes[key]; !ok {
			t.Errorf("user attribute %q was incorrectly filtered", key)
		}
	}
	if entry.Attributes["source"] != "system" {
		t.Errorf("source = %q, want %q", entry.Attributes["source"], "system")
	}
	if entry.Attributes["module"] != "etcd" {
		t.Errorf("module = %q, want %q", entry.Attributes["module"], "etcd")
	}
	if entry.Attributes["service"] != "web" {
		t.Errorf("service = %q, want %q", entry.Attributes["service"], "web")
	}
}

func TestBatchLogWriter_CloseFlushesRemaining(t *testing.T) {
	var mu sync.Mutex
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plw := NewPersistentLogWriter(srv.URL, 5*time.Second)
	bw := NewBatchLogWriter(plw)

	// Write a few entries (below threshold)
	for i := range 3 {
		err := bw.WriteEntry("drain-entity", LogEntry{
			Timestamp: time.Now(),
			Stream:    Stdout,
			Body:      "drain entry",
		})
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Close immediately — should drain the buffer
	bw.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected Close() to flush remaining entries, got none")
	}

	// Count total lines across all POSTs
	total := 0
	for _, body := range received {
		lines := strings.Split(strings.TrimSpace(body), "\n")
		total += len(lines)
	}
	if total != 3 {
		t.Fatalf("expected 3 entries flushed on close, got %d", total)
	}
}

// recordingTransport captures the request it is handed and answers 200, so
// transport wiring can be asserted without a live listener.
type recordingTransport struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (t *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.reqs = append(t.reqs, r)
	t.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
}

func (t *recordingTransport) urls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, r := range t.reqs {
		out = append(out, r.URL.String())
	}
	return out
}

func TestPersistentLogWriter_WithHTTPClient(t *testing.T) {
	rt := &recordingTransport{}
	plw := NewPersistentLogWriter("https://coordinator.invalid:8443/_telemetry/logs", 5*time.Second,
		WithHTTPClient(&http.Client{Transport: rt}))

	err := plw.WriteEntry("test-entity", LogEntry{
		Timestamp: time.Now(),
		Stream:    Stdout,
		Body:      "direct write",
	})
	if err != nil {
		t.Fatal(err)
	}

	urls := rt.urls()
	if len(urls) != 1 {
		t.Fatalf("expected the supplied client to be used once, got %d requests", len(urls))
	}
	want := "https://coordinator.invalid:8443/_telemetry/logs/insert/jsonline"
	if urls[0] != want {
		t.Fatalf("got %q, want %q", urls[0], want)
	}
}

// BatchLogWriter borrows the wrapped writer's client, so batching a writer must
// not quietly drop back to a default transport (which for a runner shipping
// through the coordinator would mean losing its credential).
func TestBatchLogWriter_InheritsHTTPClient(t *testing.T) {
	rt := &recordingTransport{}
	plw := NewPersistentLogWriter("https://coordinator.invalid:8443/_telemetry/logs", 5*time.Second,
		WithHTTPClient(&http.Client{Transport: rt}))
	bw := NewBatchLogWriter(plw)
	defer bw.Close()

	if err := bw.WriteEntry("test-entity", LogEntry{
		Timestamp: time.Now(),
		Stream:    Stdout,
		Body:      "batched write",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		if urls := rt.urls(); len(urls) > 0 {
			want := "https://coordinator.invalid:8443/_telemetry/logs/insert/jsonline"
			if urls[0] != want {
				t.Fatalf("got %q, want %q", urls[0], want)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("batch writer never flushed through the supplied client")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestBatchLogWriter_ReportsTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed up front so the send fails at the transport

	var mu sync.Mutex
	var gotErr error
	gotDropped := -1

	plw := NewPersistentLogWriter(srv.URL, time.Second)
	bw := NewBatchLogWriter(plw, WithBatchErrorHandler(func(err error, dropped int) {
		mu.Lock()
		gotErr, gotDropped = err, dropped
		mu.Unlock()
	}))
	defer bw.Close()

	for i := 0; i < 3; i++ {
		if err := bw.WriteEntry("test-entity", LogEntry{
			Timestamp: time.Now(),
			Stream:    Stdout,
			Body:      "unreachable",
		}); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotErr != nil
	}, "error handler was never called for a transport failure")

	mu.Lock()
	defer mu.Unlock()
	if gotDropped != 3 {
		t.Fatalf("reported %d dropped entries, want 3", gotDropped)
	}
}

// A rejected batch comes back as a status code, not a transport error. This is
// the shape an expired or missing credential takes, and before it was checked
// the entries vanished with no signal whatsoever.
func TestBatchLogWriter_ReportsRejectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("token expired"))
	}))
	defer srv.Close()

	var mu sync.Mutex
	var gotErr error
	gotDropped := -1

	plw := NewPersistentLogWriter(srv.URL, time.Second)
	bw := NewBatchLogWriter(plw, WithBatchErrorHandler(func(err error, dropped int) {
		mu.Lock()
		gotErr, gotDropped = err, dropped
		mu.Unlock()
	}))
	defer bw.Close()

	if err := bw.WriteEntry("test-entity", LogEntry{
		Timestamp: time.Now(),
		Stream:    Stdout,
		Body:      "rejected",
	}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotErr != nil
	}, "error handler was never called for a rejected batch")

	mu.Lock()
	defer mu.Unlock()
	if gotDropped != 1 {
		t.Fatalf("reported %d dropped entries, want 1", gotDropped)
	}
	if !strings.Contains(gotErr.Error(), "401") {
		t.Fatalf("error %q does not mention the status code", gotErr)
	}
	if !strings.Contains(gotErr.Error(), "token expired") {
		t.Fatalf("error %q does not quote the response body", gotErr)
	}
}

// A writer with no error handler keeps its historical silence rather than
// panicking on the nil callback.
func TestBatchLogWriter_SilentWithoutHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	plw := NewPersistentLogWriter(srv.URL, time.Second)
	bw := NewBatchLogWriter(plw)
	defer bw.Close()

	if err := bw.WriteEntry("test-entity", LogEntry{
		Timestamp: time.Now(),
		Stream:    Stdout,
		Body:      "rejected",
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(defaultFlushInterval * 3)
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal(msg)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
