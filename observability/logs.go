package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miren.dev/runtime/pkg/asm/autoreg"
)

// normalizeBaseURL ensures the address has a scheme and no trailing slash
func normalizeBaseURL(address string) string {
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return strings.TrimRight(address, "/")
}

type LogStream string

const (
	Stdout  LogStream = "stdout"
	Stderr  LogStream = "stderr"
	Error   LogStream = "error"
	UserOOB LogStream = "user-oob"
)

type LogEntry struct {
	Timestamp  time.Time
	Stream     LogStream
	TraceID    string
	Attributes map[string]string
	Body       string
}

type PersistentLogWriter struct {
	Address string        `asm:"victorialogs-address"`
	Timeout time.Duration `asm:"victorialogs-timeout"`

	client *http.Client
}

var _ = autoreg.Register[PersistentLogWriter]()

func (l *PersistentLogWriter) Populated() error {
	if l.Timeout == 0 {
		l.Timeout = 30 * time.Second
	}

	l.client = &http.Client{
		Timeout: l.Timeout,
	}
	return nil
}

func (l *PersistentLogWriter) WriteEntry(entity string, le LogEntry) error {
	// Convert LogEntry to VictoriaLogs JSON format
	logData := map[string]interface{}{
		"_msg":     le.Body,
		"_time":    le.Timestamp.UTC().Format(time.RFC3339Nano),
		"entity":   entity,
		"stream":   string(le.Stream),
		"trace_id": le.TraceID,
	}

	// Add all attributes as top-level fields
	for k, v := range le.Attributes {
		logData[k] = v
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(logData)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	// Add newline for JSON lines format
	jsonData = append(jsonData, '\n')

	// Send to VictoriaLogs
	baseURL := normalizeBaseURL(l.Address)
	insertURL := baseURL + "/insert/jsonline"
	resp, err := l.client.Post(insertURL, "application/x-ndjson", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send log to victorialogs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victorialogs returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

type PersistentLogReader struct {
	Address string        `asm:"victorialogs-address"`
	Timeout time.Duration `asm:"victorialogs-timeout"`

	client *http.Client
}

func (l *PersistentLogReader) Populated() error {
	if l.Timeout == 0 {
		l.Timeout = 30 * time.Second
	}

	l.client = &http.Client{
		Timeout: l.Timeout,
	}
	return nil
}

func (l *PersistentLogReader) Read(ctx context.Context, id string) ([]LogEntry, error) {
	reader := &LogReader{
		Address: l.Address,
		client:  l.client,
	}
	return reader.Read(ctx, id)
}

type LogsMaintainer struct {
}

var _ = autoreg.Register[LogsMaintainer]()

func (m *LogsMaintainer) Setup(ctx context.Context) error {
	// VictoriaLogs is schemaless, no setup needed
	return nil
}

type LogWriter interface {
	WriteEntry(entity string, le LogEntry) error
}

type DebugLogWriter struct {
	Log *slog.Logger
}

func (d *DebugLogWriter) WriteEntry(entity string, le LogEntry) error {
	d.Log.Debug(le.Body, "stream", le.Stream, "entity", entity)
	return nil
}

type LogReader struct {
	Address string        `asm:"victorialogs-address"`
	Timeout time.Duration `asm:"victorialogs-timeout"`

	client *http.Client
}

var _ = autoreg.Register[LogReader]()

func (l *LogReader) Populated() error {
	if l.Timeout == 0 {
		l.Timeout = 30 * time.Second
	}

	l.client = &http.Client{
		Timeout: l.Timeout,
	}
	return nil
}

type logReadOpts struct {
	From  time.Time
	Limit int
}

type LogReaderOption func(*logReadOpts)

func WithFromTime(t time.Time) LogReaderOption {
	return func(o *logReadOpts) {
		o.From = t
	}
}

func WithLimit(l int) LogReaderOption {
	return func(o *logReadOpts) {
		o.Limit = l
	}
}

func logsQLQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func (l *LogReader) Read(ctx context.Context, id string, opts ...LogReaderOption) ([]LogEntry, error) {
	var o logReadOpts

	for _, opt := range opts {
		opt(&o)
	}

	limit := o.Limit
	if limit == 0 {
		limit = 100
	}

	// Build LogsQL query - use simple field matching
	query := `entity:` + logsQLQuote(id)

	// Victoria Logs often requires a time range
	// If not provided, use last 24 hours
	startTime := o.From
	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}

	return l.executeQuery(ctx, query, limit, startTime, time.Now())
}

func (l *LogReader) ReadBySandbox(ctx context.Context, sandboxID string, opts ...LogReaderOption) ([]LogEntry, error) {
	var o logReadOpts

	for _, opt := range opts {
		opt(&o)
	}

	limit := o.Limit
	if limit == 0 {
		limit = 100
	}

	// Build LogsQL query filtering by sandbox attribute
	query := `sandbox:` + logsQLQuote(sandboxID)

	// Victoria Logs often requires a time range
	startTime := o.From
	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}

	return l.executeQuery(ctx, query, limit, startTime, time.Now())
}

func (l *LogReader) executeQuery(ctx context.Context, query string, limit int, start, end time.Time) ([]LogEntry, error) {
	// VictoriaLogs uses /select/logsql/query for queries
	baseURL := normalizeBaseURL(l.Address)
	queryURL := baseURL + "/select/logsql/query"

	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", fmt.Sprintf("%d", limit))
	// Add time range - VictoriaLogs uses RFC3339 timestamps
	params.Set("start", start.Format(time.RFC3339Nano))
	params.Set("end", end.Format(time.RFC3339Nano))

	fullURL := fmt.Sprintf("%s?%s", queryURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query victorialogs: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("victorialogs returned status %d: %s", resp.StatusCode, string(body))
	}

	// VictoriaLogs returns NDJSON (newline-delimited JSON)
	var entries []LogEntry

	// If empty response, return empty list
	if len(bytes.TrimSpace(body)) == 0 {
		return entries, nil
	}

	// Split by newlines and parse each line
	lines := bytes.Split(body, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var logData map[string]interface{}
		if err := json.Unmarshal(line, &logData); err != nil {
			// Log the error but continue - might be partial data
			continue
		}

		entry := LogEntry{
			Attributes: make(map[string]string),
		}

		// Parse standard fields
		if msg, ok := logData["_msg"].(string); ok {
			entry.Body = msg
		}

		if timeStr, ok := logData["_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
				entry.Timestamp = t
			}
		}

		if stream, ok := logData["stream"].(string); ok {
			entry.Stream = LogStream(stream)
		}

		if traceID, ok := logData["trace_id"].(string); ok {
			entry.TraceID = traceID
		}

		// Add all other fields as attributes
		for k, v := range logData {
			if k == "_msg" || k == "_time" || k == "stream" || k == "trace_id" || k == "entity" {
				continue
			}
			if strVal, ok := v.(string); ok {
				entry.Attributes[k] = strVal
			}
		}

		entries = append(entries, entry)
	}

	return entries, nil
}
