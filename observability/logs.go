package observability

import (
	"bufio"
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

func (l *PersistentLogWriter) Client() *http.Client {
	if l.client == nil {
		return http.DefaultClient
	}

	return l.client
}

func (l *PersistentLogWriter) WriteEntry(entity string, le LogEntry) error {
	// VictoriaLogs requires a non-empty _msg field but we want to preserve
	// empty log messages because they'll show up as blank lines in the output.
	// So use a single space if empty
	msg := le.Body
	if msg == "" {
		msg = " "
	}

	// Convert LogEntry to VictoriaLogs JSON format
	logData := map[string]any{
		"_msg":     msg,
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
	resp, err := l.Client().Post(insertURL, "application/x-ndjson", bytes.NewReader(jsonData))
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

const DefaultLogReadLimit = 1000

func (l *LogReader) Read(ctx context.Context, id string, opts ...LogReaderOption) ([]LogEntry, error) {
	var o logReadOpts

	for _, opt := range opts {
		opt(&o)
	}

	limit := o.Limit
	if limit == 0 {
		limit = DefaultLogReadLimit
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
		limit = DefaultLogReadLimit
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

// LogTarget specifies what logs to query - either by entity ID or sandbox ID.
type LogTarget struct {
	EntityID  string
	SandboxID string
	Filter    string // Optional LogsQL filter expression (e.g., "error" or ~"regex")
}

// Query returns the LogsQL query string for this target.
func (t LogTarget) Query() string {
	var base string
	if t.SandboxID != "" {
		base = `sandbox:` + logsQLQuote(t.SandboxID)
	} else {
		base = `entity:` + logsQLQuote(t.EntityID)
	}

	if t.Filter != "" {
		// Append filter to query - VictoriaLogs LogsQL syntax
		// User can specify word filters, phrase filters ("phrase"), or regex (~"pattern")
		return base + " " + t.Filter
	}
	return base
}

// ReadStream queries historical logs and sends them to a channel as they're parsed.
// Unlike Read(), this has no limit and streams results incrementally.
func (l *LogReader) ReadStream(ctx context.Context, target LogTarget, logCh chan<- LogEntry, opts ...LogReaderOption) error {
	return l.executeStreamQuery(ctx, target.Query(), logCh, opts...)
}

// TailStream connects to VictoriaLogs tail endpoint for live tailing.
// Blocks until context is cancelled.
func (l *LogReader) TailStream(ctx context.Context, target LogTarget, logCh chan<- LogEntry, opts ...LogReaderOption) error {
	return l.executeTailQuery(ctx, target.Query(), logCh, opts...)
}

func (l *LogReader) executeStreamQuery(ctx context.Context, query string, logCh chan<- LogEntry, opts ...LogReaderOption) error {
	var o logReadOpts
	for _, opt := range opts {
		opt(&o)
	}

	baseURL := normalizeBaseURL(l.Address)
	queryURL := baseURL + "/select/logsql/query"

	// Sort by time ascending so older logs appear first
	sortedQuery := query + " | sort by (_time) asc"

	params := url.Values{}
	params.Set("query", sortedQuery)
	// No limit - stream all results

	startTime := o.From
	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}
	params.Set("start", startTime.Format(time.RFC3339Nano))
	params.Set("end", time.Now().Format(time.RFC3339Nano))

	fullURL := fmt.Sprintf("%s?%s", queryURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Use a client without timeout for streaming
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to query victorialogs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victorialogs returned status %d: %s", resp.StatusCode, string(body))
	}

	return l.parseLogStream(ctx, resp.Body, logCh)
}

func (l *LogReader) executeTailQuery(ctx context.Context, query string, logCh chan<- LogEntry, opts ...LogReaderOption) error {
	var o logReadOpts
	for _, opt := range opts {
		opt(&o)
	}

	baseURL := normalizeBaseURL(l.Address)
	tailURL := baseURL + "/select/logsql/tail"

	params := url.Values{}
	params.Set("query", query)

	// Include historical logs if From time specified
	if !o.From.IsZero() {
		offset := time.Since(o.From)
		params.Set("start_offset", offset.String())
	}

	fullURL := fmt.Sprintf("%s?%s", tailURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Use a client without timeout for streaming
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to victorialogs tail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("victorialogs returned status %d: %s", resp.StatusCode, string(body))
	}

	return l.parseLogStream(ctx, resp.Body, logCh)
}

func (l *LogReader) parseLogStream(ctx context.Context, body io.Reader, logCh chan<- LogEntry) error {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		entry, err := l.parseLogLine(line)
		if err != nil {
			// Skip malformed lines
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case logCh <- entry:
		}
	}

	return scanner.Err()
}

func (l *LogReader) parseLogLine(line []byte) (LogEntry, error) {
	var logData map[string]interface{}
	if err := json.Unmarshal(line, &logData); err != nil {
		return LogEntry{}, err
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

	return entry, nil
}

func (l *LogReader) executeQuery(ctx context.Context, query string, limit int, start, end time.Time) ([]LogEntry, error) {
	// VictoriaLogs uses /select/logsql/query for queries
	baseURL := normalizeBaseURL(l.Address)
	queryURL := baseURL + "/select/logsql/query"

	// Sort by time ascending so older logs appear first
	sortedQuery := query + " | sort by (_time) asc"

	params := url.Values{}
	params.Set("query", sortedQuery)
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

		entry, err := l.parseLogLine(line)
		if err != nil {
			// Skip malformed lines
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}
