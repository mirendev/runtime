package sandbox

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/observability"
)

type mockLogWriter struct {
	entries []mockLogEntry
}

type mockLogEntry struct {
	entity string
	log    observability.LogEntry
}

func (m *mockLogWriter) WriteEntry(entity string, le observability.LogEntry) error {
	m.entries = append(m.entries, mockLogEntry{
		entity: entity,
		log:    le,
	})
	return nil
}

func TestSandboxLogs(t *testing.T) {
	t.Run("processes stdout lines", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		// Write some lines
		input := []byte("line 1\nline 2\nline 3\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 3)
		r.Equal("line 1", mock.entries[0].log.Body)
		r.Equal("line 2", mock.entries[1].log.Body)
		r.Equal("line 3", mock.entries[2].log.Body)

		for i, entry := range mock.entries {
			r.Equal(entityID, entry.entity, "entry %d should have correct entity", i)
			r.Equal(observability.Stdout, entry.log.Stream, "entry %d should be stdout", i)
		}
	})

	t.Run("buffers partial lines", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		// Write partial line
		n, err := sl.Write([]byte("partial"))
		r.NoError(err)
		r.Equal(7, n)

		// Should not have written anything yet
		r.Len(mock.entries, 0)

		// Complete the line
		n, err = sl.Write([]byte(" line\n"))
		r.NoError(err)
		r.Equal(6, n)

		// Now should have one entry
		r.Len(mock.entries, 1)
		r.Equal("partial line", mock.entries[0].log.Body)
	})

	t.Run("caps an unterminated firehose line", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		// Stream ~12.5 MiB with no newline, in 64 KiB chunks the way containerd
		// pumps a blob — far more than the 1 MiB cap.
		chunk := bytes.Repeat([]byte("x"), 64*1024)
		for range 200 {
			n, err := sl.Write(chunk)
			r.NoError(err)
			r.Equal(len(chunk), n) // all bytes are always "consumed"
		}

		// Nothing flushed yet (no newline), and the buffer never exceeded the cap.
		r.Len(mock.entries, 0)
		r.LessOrEqual(sl.buf.Len(), maxLineBytes)

		// A newline flushes the line, truncated to the cap rather than ~12 MiB.
		_, err := sl.Write([]byte("\n"))
		r.NoError(err)
		r.Len(mock.entries, 1)
		r.Equal(maxLineBytes, len(mock.entries[0].log.Body))
	})

	t.Run("handles USER prefix", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		input := []byte("!USER this is a user log\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 1)
		r.Equal("this is a user log", mock.entries[0].log.Body)
		r.Equal(observability.UserOOB, mock.entries[0].log.Stream)
	})

	t.Run("handles ERROR prefix", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		input := []byte("!ERROR this is an error log\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 1)
		r.Equal("this is an error log", mock.entries[0].log.Body)
		r.Equal(observability.Error, mock.entries[0].log.Stream)
	})

	t.Run("extracts trace ID from log", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()
		traceID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		input := []byte("log with trace_id=" + traceID + "\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 1)
		r.Equal(traceID, mock.entries[0].log.TraceID)
	})

	t.Run("includes attributes in logs", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		attrs := map[string]string{
			"miren.sandbox":   "test-sandbox",
			"miren.container": "test-container",
			"miren.version":   "v1.0.0",
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, attrs, mock)

		input := []byte("log with attributes\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 1)
		r.Equal(attrs, mock.entries[0].log.Attributes)
	})

	t.Run("Stderr returns clone with stderr stream", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		stderr := sl.Stderr()

		// Write to stderr clone
		input := []byte("error line\n")
		n, err := stderr.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 1)
		r.Equal("error line", mock.entries[0].log.Body)
		r.Equal(observability.Stderr, mock.entries[0].log.Stream)

		// Original should still be stdout
		input2 := []byte("stdout line\n")
		n, err = sl.Write(input2)
		r.NoError(err)
		r.Equal(len(input2), n)

		r.Len(mock.entries, 2)
		r.Equal("stdout line", mock.entries[1].log.Body)
		r.Equal(observability.Stdout, mock.entries[1].log.Stream)
	})

	t.Run("handles multiple lines in single write", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		input := []byte("line 1\nline 2\nline 3\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 3)
		r.Equal("line 1", mock.entries[0].log.Body)
		r.Equal("line 2", mock.entries[1].log.Body)
		r.Equal("line 3", mock.entries[2].log.Body)
	})

	t.Run("trims trailing newlines and tabs", func(t *testing.T) {
		r := require.New(t)

		mock := &mockLogWriter{}
		entityID := identity.NewID()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

		input := []byte("line with trailing\t\r\n")
		n, err := sl.Write(input)
		r.NoError(err)
		r.Equal(len(input), n)

		r.Len(mock.entries, 1)
		r.Equal("line with trailing", mock.entries[0].log.Body)
	})
}

func BenchmarkSandboxLogs(b *testing.B) {
	mock := &mockLogWriter{}
	entityID := identity.NewID()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

	input := []byte("benchmark log line\n")

	for b.Loop() {
		sl.Write(input)
	}
}

// nopLogWriter discards entries so benchmarks measure the ingest/parse path
// without the allocation noise of mockLogWriter's growing slice.
type nopLogWriter struct{}

func (nopLogWriter) WriteEntry(string, observability.LogEntry) error { return nil }

// BenchmarkSandboxLogsByLineKind isolates the cost of the JSON ingest path
// added for structured-log parsing. The plain case hits the early scanJSON
// bail; the JSON cases exercise the json.Decoder tokenizer, which allocates a
// decoder plus per-token strings on the hottest path in the system. Run with
// -benchmem to see allocs/op.
//
// The JSON shapes are modeled on a real sample of garden/club app logs: Go
// slog lines (string time/level) carry 1-2 fields in the common case and ~5
// for HTTP access logs, while pino/Node lines (numeric time/level) wrap nested
// req/res objects that exercise skipNestedJSON. In that sample only ~0.5% of
// total volume was JSON at all, so the plain fast-path dominates in aggregate;
// these cases measure the per-line cost an app pays when it does log structured.
func BenchmarkSandboxLogsByLineKind(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	entityID := identity.NewID()

	cases := []struct {
		name string
		line string
	}{
		{"plain", "a plain unstructured log line\n"},
		// Go slog, msg+level+time plus 1-2 fields — the ~93% common case.
		{
			"slog_narrow",
			`{"time":"2026-06-04T16:05:55Z","level":"INFO","msg":"Inactive player cleanup completed",` +
				`"totalPlayers":0,"cleanedPlayers":0}` + "\n",
		},
		// Go slog HTTP access log — the realistic flat "wide" case (~5 fields).
		{
			"slog_request",
			`{"time":"2026-06-04T14:11:05Z","level":"INFO","msg":"request","method":"GET",` +
				`"path":"/.well-known/openid-configuration","status":200,"duration_ms":0,"remote_addr":"10.0.0.1:41436"}` + "\n",
		},
		// pino/Node, numeric level/time plus a nested req object — exercises skipNestedJSON.
		{
			"pino_nested",
			`{"level":30,"time":1780585649689,"pid":1,"hostname":"node-1","name":"app",` +
				`"req":{"id":27550,"method":"GET","url":"/wp-admin/install.php","query":{"step":"1"},` +
				`"headers":{"host":"example.com","user-agent":"curl/8.0"}},"msg":"incoming request"}` + "\n",
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sl := NewSandboxLogs(logger, entityID, map[string]string{}, nopLogWriter{})
			input := []byte(tc.line)
			b.ReportAllocs()
			for b.Loop() {
				sl.Write(input)
			}
		})
	}
}

func TestScanJSON(t *testing.T) {
	newScanner := func() *SandboxLogs {
		return NewSandboxLogs(
			slog.New(slog.NewTextHandler(os.Stderr, nil)),
			"test", map[string]string{}, &mockLogWriter{},
		)
	}

	t.Run("valid JSON with msg", func(t *testing.T) {
		sl := newScanner()
		body, stream, ok := sl.scanJSON(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"hello","key":"val"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if body != "hello" {
			t.Errorf("body = %q, want %q", body, "hello")
		}
		if sl.extra["key"] != "val" {
			t.Errorf("extra[key] = %q, want %q", sl.extra["key"], "val")
		}
		for _, skip := range []string{"time", "level", "msg"} {
			if _, exists := sl.extra[skip]; exists {
				t.Errorf("field %q should be stripped", skip)
			}
		}
		if stream != "" {
			t.Errorf("stream = %q, want empty for INFO", stream)
		}
	})

	t.Run("valid JSON with message field", func(t *testing.T) {
		sl := newScanner()
		body, _, ok := sl.scanJSON(`{"time":"2026-01-01T00:00:00Z","level":"INFO","message":"hello"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if body != "hello" {
			t.Errorf("body = %q, want %q", body, "hello")
		}
	})

	t.Run("ERROR level sets stderr stream", func(t *testing.T) {
		sl := newScanner()
		_, stream, ok := sl.scanJSON(`{"level":"ERROR","msg":"fail"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if stream != observability.Stderr {
			t.Errorf("stream = %q, want stderr", stream)
		}
	})

	t.Run("WARN level sets stderr stream", func(t *testing.T) {
		sl := newScanner()
		_, stream, ok := sl.scanJSON(`{"level":"WARN","msg":"warning"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if stream != observability.Stderr {
			t.Errorf("stream = %q, want stderr", stream)
		}
	})

	t.Run("non-JSON returns false", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON("plain text")
		if ok {
			t.Error("expected ok=false for plain text")
		}
	})

	t.Run("JSON without msg field returns false", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"key":"value","other":"data"}`)
		if ok {
			t.Error("expected ok=false for JSON without msg")
		}
	})

	t.Run("non-string values are formatted", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"msg":"hi","count":42,"flag":true}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if sl.extra["count"] != "42" {
			t.Errorf("extra[count] = %q, want %q", sl.extra["count"], "42")
		}
		if sl.extra["flag"] != "true" {
			t.Errorf("extra[flag] = %q, want %q", sl.extra["flag"], "true")
		}
	})

	t.Run("large numbers preserve original literal", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"msg":"hi","big":1000000,"id":9007199254740993}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if sl.extra["big"] != "1000000" {
			t.Errorf("extra[big] = %q, want %q", sl.extra["big"], "1000000")
		}
		if sl.extra["id"] != "9007199254740993" {
			t.Errorf("extra[id] = %q, want %q", sl.extra["id"], "9007199254740993")
		}
	})

	t.Run("nested objects are skipped", func(t *testing.T) {
		sl := newScanner()
		body, _, ok := sl.scanJSON(`{"msg":"hi","nested":{"a":1},"after":"yes"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if body != "hi" {
			t.Errorf("body = %q, want %q", body, "hi")
		}
		if _, exists := sl.extra["nested"]; exists {
			t.Error("nested objects should be skipped")
		}
		if sl.extra["after"] != "yes" {
			t.Errorf("extra[after] = %q, want %q", sl.extra["after"], "yes")
		}
	})

	t.Run("miren. prefixed keys are escaped", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"msg":"hi","miren.sandbox":"evil","safe":"ok"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if _, exists := sl.extra["miren.sandbox"]; exists {
			t.Error("miren.sandbox should be escaped, not stored directly")
		}
		if sl.extra["-miren.sandbox"] != "evil" {
			t.Errorf("extra[-miren.sandbox] = %q, want %q", sl.extra["-miren.sandbox"], "evil")
		}
		if sl.extra["safe"] != "ok" {
			t.Errorf("extra[safe] = %q, want %q", sl.extra["safe"], "ok")
		}
	})

	t.Run("un-namespaced source key is escaped", func(t *testing.T) {
		// source is the one internal attribute that isn't under the miren.*
		// namespace, so it gets the same dash-escape treatment.
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"msg":"hi","source":"evil"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if _, exists := sl.extra["source"]; exists {
			t.Error("source should be escaped, not stored directly")
		}
		if sl.extra["-source"] != "evil" {
			t.Errorf("extra[-source] = %q, want %q", sl.extra["-source"], "evil")
		}
	})

	t.Run("rejects trailing content after JSON object", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"msg":"ok"} trailing`)
		if ok {
			t.Error("expected ok=false for JSON with trailing content")
		}
	})

	t.Run("reuses extra map across calls", func(t *testing.T) {
		sl := newScanner()
		sl.scanJSON(`{"msg":"first","aaa":"1"}`)
		sl.scanJSON(`{"msg":"second","bbb":"2"}`)
		if _, exists := sl.extra["aaa"]; exists {
			t.Error("extra from first call should be cleared")
		}
		if sl.extra["bbb"] != "2" {
			t.Errorf("extra[bbb] = %q, want %q", sl.extra["bbb"], "2")
		}
	})

	t.Run("unescapes string escapes and unicode", func(t *testing.T) {
		sl := newScanner()
		body, _, ok := sl.scanJSON(`{"msg":"line1\nline2\t\"q\"","path":"a\/b","emoji":"☃"}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if body != "line1\nline2\t\"q\"" {
			t.Errorf("body = %q, want escapes decoded", body)
		}
		// \/ is a valid JSON escape that Go's strconv.Unquote rejects; gjson
		// handles it, which is part of why we don't hand-roll unescaping.
		if sl.extra["path"] != "a/b" {
			t.Errorf("extra[path] = %q, want %q", sl.extra["path"], "a/b")
		}
		if sl.extra["emoji"] != "☃" {
			t.Errorf("extra[emoji] = %q, want snowman (multibyte UTF-8 passthrough)", sl.extra["emoji"])
		}
	})

	t.Run("float literal preserved", func(t *testing.T) {
		sl := newScanner()
		_, _, ok := sl.scanJSON(`{"msg":"hi","responseTime":0.6435079574584961}`)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if sl.extra["responseTime"] != "0.6435079574584961" {
			t.Errorf("extra[responseTime] = %q, want exact float literal", sl.extra["responseTime"])
		}
	})
}

func TestProcessLineJSON(t *testing.T) {
	mock := &mockLogWriter{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	entityID := "test-entity"
	baseAttrs := map[string]string{"miren.sandbox": "sandbox/test-abc"}

	sl := NewSandboxLogs(logger, entityID, baseAttrs, mock)
	sl.Write([]byte(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"processing step","component":"provisioner","cluster_id":"ZA8"}` + "\n"))

	if len(mock.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(mock.entries))
	}

	entry := mock.entries[0].log
	if entry.Body != "processing step" {
		t.Errorf("body = %q, want %q", entry.Body, "processing step")
	}
	if entry.Extra["component"] != "provisioner" {
		t.Errorf("extra[component] = %q, want %q", entry.Extra["component"], "provisioner")
	}
	if entry.Extra["cluster_id"] != "ZA8" {
		t.Errorf("extra[cluster_id] = %q, want %q", entry.Extra["cluster_id"], "ZA8")
	}
	if entry.Attributes["miren.sandbox"] != "sandbox/test-abc" {
		t.Errorf("base attrs should be preserved: attrs[miren.sandbox] = %q", entry.Attributes["miren.sandbox"])
	}
	if _, hasTime := entry.Extra["time"]; hasTime {
		t.Error("time field should be stripped from extra")
	}
}

func BenchmarkSandboxLogsLargeBuffer(b *testing.B) {
	mock := &mockLogWriter{}
	entityID := identity.NewID()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sl := NewSandboxLogs(logger, entityID, map[string]string{}, mock)

	// Create a large buffer with many lines
	var buf bytes.Buffer
	for i := range 100 {
		buf.WriteString("log line ")
		buf.WriteString(string('0' + rune(i%10)))
		buf.WriteByte('\n')
	}
	input := buf.Bytes()

	for b.Loop() {
		sl.Write(input)
		mock.entries = mock.entries[:0] // Reset
	}
}
