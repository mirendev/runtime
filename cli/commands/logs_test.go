package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

func TestBuildFilterWithService(t *testing.T) {
	tests := []struct {
		name       string
		userFilter string
		service    string
		want       string
	}{
		{
			name:       "no service, no filter",
			userFilter: "",
			service:    "",
			want:       "",
		},
		{
			name:       "service only",
			userFilter: "",
			service:    "web",
			want:       `(service:"web" OR miren.service:"web")`,
		},
		{
			name:       "filter only",
			userFilter: "error",
			service:    "",
			want:       "error",
		},
		{
			name:       "service and filter",
			userFilter: "error",
			service:    "web",
			want:       `(service:"web" OR miren.service:"web") error`,
		},
		{
			name:       "service with complex filter",
			userFilter: `error -debug /timeout/`,
			service:    "worker",
			want:       `(service:"worker" OR miren.service:"worker") error -debug /timeout/`,
		},
		{
			name:       "service with spaces needs quoting",
			userFilter: "",
			service:    "my service",
			want:       `(service:"my service" OR miren.service:"my service")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilterWithService(tt.userFilter, tt.service)
			if got != tt.want {
				t.Errorf("buildFilterWithService(%q, %q) = %q, want %q",
					tt.userFilter, tt.service, got, tt.want)
			}
		})
	}
}

// TestBuildFilterWithService_LegacyCompatibility verifies the contract that
// dispatchLogs depends on: when no --service flag is passed, combinedFilter
// must equal rawFilter so the legacy capability check (rawFilter != combinedFilter)
// doesn't falsely trip. If this test fails, older servers without streamLogChunks
// will reject plain `miren logs` requests.
func TestBuildFilterWithService_LegacyCompatibility(t *testing.T) {
	for _, userFilter := range []string{"", "error", `"exact phrase"`, `error -debug /timeout/`} {
		combined := buildFilterWithService(userFilter, "")
		if combined != userFilter {
			t.Errorf("buildFilterWithService(%q, \"\") = %q, want %q — "+
				"no-service filter must equal rawFilter for legacy server compat",
				userFilter, combined, userFilter)
		}
	}
}

func TestNormalizeSandboxID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc123", "sandbox/abc123"},
		{"sandbox/abc123", "sandbox/abc123"},
		{"", "sandbox/"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeSandboxID(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSandboxID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildBuildFilter(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		userFilter string
		want       string
	}{
		{
			name:    "version only",
			version: "v3",
			want:    `source:build version:"v3"`,
		},
		{
			name:       "version with filter",
			version:    "v3",
			userFilter: "error",
			want:       `source:build version:"v3" error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBuildFilter(tt.version, tt.userFilter)
			if got != tt.want {
				t.Errorf("buildBuildFilter(%q, %q) = %q, want %q",
					tt.version, tt.userFilter, got, tt.want)
			}
		})
	}
}

func TestPrintLogEntryJSON(t *testing.T) {
	ts := time.Date(2026, 3, 13, 16, 30, 0, 0, time.UTC)

	t.Run("full entry", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stdout")
		entry.SetSource("sandbox/abc123")
		entry.SetLine("Hello world")
		entry.SetAttributes(map[string]string{"service": "web"})

		printLogEntryJSON(ctx, entry)

		var got logEntryJSON
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
		}

		if got.Timestamp != "2026-03-13T16:30:00Z" {
			t.Errorf("timestamp = %q, want %q", got.Timestamp, "2026-03-13T16:30:00Z")
		}
		if got.Stream != "stdout" {
			t.Errorf("stream = %q, want %q", got.Stream, "stdout")
		}
		if got.Source != "sandbox/abc123" {
			t.Errorf("source = %q, want %q", got.Source, "sandbox/abc123")
		}
		if got.Message != "Hello world" {
			t.Errorf("message = %q, want %q", got.Message, "Hello world")
		}
		if got.Attributes["service"] != "web" {
			t.Errorf("attributes[service] = %q, want %q", got.Attributes["service"], "web")
		}
	})

	t.Run("minimal entry omits empty fields", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stderr")
		entry.SetLine("error occurred")

		printLogEntryJSON(ctx, entry)

		raw := strings.TrimSpace(buf.String())
		if strings.Contains(raw, "source") {
			t.Errorf("expected source to be omitted, got: %s", raw)
		}
		if strings.Contains(raw, "attributes") {
			t.Errorf("expected attributes to be omitted, got: %s", raw)
		}
	})

	t.Run("multiple entries produce JSONL", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		for i := 0; i < 3; i++ {
			entry := &app_v1alpha.LogEntry{}
			entry.SetTimestamp(standard.ToTimestamp(ts))
			entry.SetStream("stdout")
			entry.SetLine("line")
			printLogEntryJSON(ctx, entry)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d", len(lines))
		}
		for i, line := range lines {
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				t.Errorf("line %d is not valid JSON: %v", i, err)
			}
		}
	})
}

func TestPrintLogEntry(t *testing.T) {
	ts := time.Date(2026, 3, 13, 16, 30, 0, 0, time.UTC)

	t.Run("uses service.id as prefix", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stdout")
		entry.SetSource("clusteragent-web-CbZwTC2ATZnrWjt1uPwog")
		entry.SetLine("hello world")
		entry.SetAttributes(map[string]string{"miren.service": "web", "miren.short_id": "wog"})

		printLogEntry(ctx, entry)

		got := buf.String()
		if !strings.Contains(got, "web.wog") {
			t.Errorf("expected service.id prefix web.wog in output, got: %s", got)
		}
		if strings.Contains(got, "miren.short_id=") || strings.Contains(got, "miren.service=") {
			t.Errorf("miren.* should be hidden from attributes, got: %s", got)
		}
	})

	t.Run("uses short_id alone when no service", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stdout")
		entry.SetSource("clusteragent-web-CbZwTC2ATZnrWjt1uPwog")
		entry.SetLine("hello world")
		entry.SetAttributes(map[string]string{"miren.short_id": "wog"})

		printLogEntry(ctx, entry)

		got := buf.String()
		if !strings.Contains(got, "wog") || strings.Contains(got, "[wog]") {
			t.Errorf("expected bare short_id prefix (no brackets), got: %s", got)
		}
	})

	t.Run("falls back to full source without short_id", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stdout")
		entry.SetSource("clusteragent-web-CbZwTC2ATZnrWjt1uPwog")
		entry.SetLine("hello world")

		printLogEntry(ctx, entry)

		got := buf.String()
		// The full source is shown, not clipped: the readable part is at the front.
		if !strings.Contains(got, "clusteragent-web-CbZwTC2ATZnrWjt1uPwog") {
			t.Errorf("expected full source in output, got: %s", got)
		}
	})

	t.Run("filters noise attributes", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stdout")
		entry.SetLine("hello")
		entry.SetAttributes(map[string]string{
			"miren.container": "app",
			"miren.sandbox":   "sandbox/test-abc123",
			"miren.service":   "web",
			"miren.stage":     "app-run",
			"miren.version":   "app_version/test-v1",
			"miren.short_id":  "abc",
			"component":       "scheduler",
		})

		printLogEntry(ctx, entry)

		got := buf.String()
		if !strings.Contains(got, "component=scheduler") {
			t.Errorf("expected component=scheduler in output, got: %s", got)
		}
		if strings.Contains(got, "miren.") {
			t.Errorf("miren.* attributes should be hidden, got: %s", got)
		}
	})

	t.Run("plain text lines displayed as-is", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}

		entry := &app_v1alpha.LogEntry{}
		entry.SetTimestamp(standard.ToTimestamp(ts))
		entry.SetStream("stdout")
		entry.SetLine("plain text log message")

		printLogEntry(ctx, entry)

		got := buf.String()
		if !strings.Contains(got, "plain text log message") {
			t.Errorf("expected plain text preserved, got: %s", got)
		}
	})
}

func TestRouterLineRendering(t *testing.T) {
	ts := time.Date(2026, 3, 13, 16, 30, 0, 0, time.UTC)

	mkRouter := func(attrs map[string]string) *app_v1alpha.LogEntry {
		e := &app_v1alpha.LogEntry{}
		e.SetTimestamp(standard.ToTimestamp(ts))
		e.SetStream("user-oob")
		e.SetSource("router")
		e.SetLine(`status=200 method=GET path="/api/v1/self" duration_ms=36 response=392 host=miren.cloud`)
		e.SetAttributes(attrs)
		return e
	}

	t.Run("does not double-print body fields from attributes", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}
		// These attributes duplicate what the logfmt body already carries.
		printLogEntry(ctx, mkRouter(map[string]string{
			"source": "router", "method": "GET", "path": "/api/v1/self", "host": "miren.cloud",
		}))
		got := buf.String()
		if n := strings.Count(got, "method=GET"); n != 1 {
			t.Errorf("method=GET should appear exactly once (body only), got %d in: %s", n, got)
		}
		if n := strings.Count(got, "host=miren.cloud"); n != 1 {
			t.Errorf("host=miren.cloud should appear exactly once, got %d in: %s", n, got)
		}
		if !strings.Contains(got, "router") {
			t.Errorf("expected router prefix, got: %s", got)
		}
	})

	t.Run("router body is byte-identical with color off (peek==pipe)", func(t *testing.T) {
		// A quoted value containing a space exercises the tokenizer's quote
		// handling; with color off the styled form must equal the input exactly.
		body := `status=404 method=GET path="/a b/c" duration_ms=3 response=12 host=x.example app.user=usr-1`
		if got := colorizeRouterBody(body); got != body {
			t.Errorf("colorizeRouterBody altered bytes with color off:\n in: %q\nout: %q", body, got)
		}
	})

	t.Run("unterminated quote preserves bytes", func(t *testing.T) {
		// A malformed/truncated body with an unclosed quote must not drop bytes:
		// the value consumes to the end of the string.
		body := `status=200 method=GET path="/truncated`
		if got := colorizeRouterBody(body); got != body {
			t.Errorf("colorizeRouterBody dropped bytes on unterminated quote:\n in: %q\nout: %q", body, got)
		}
	})

	t.Run("internal router access field is not double-printed", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}
		e := &app_v1alpha.LogEntry{}
		e.SetTimestamp(standard.ToTimestamp(ts))
		e.SetStream("user-oob")
		e.SetSource("router")
		e.SetLine(`status=200 method=GET path="/internal" access=internal duration_ms=2 response=10`)
		// The router emits access in both the body and attributes.
		e.SetAttributes(map[string]string{
			"source": "router", "access": "internal", "method": "GET", "path": "/internal",
		})
		printLogEntry(ctx, e)
		if n := strings.Count(buf.String(), "access=internal"); n != 1 {
			t.Errorf("access=internal should appear once (body only), got %d in: %s", n, buf.String())
		}
	})

	t.Run("promoted app.* fields still render", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := &Context{Context: context.Background(), Stdout: &buf}
		printLogEntry(ctx, mkRouter(map[string]string{
			"source": "router", "method": "GET", "path": "/api/v1/self", "host": "miren.cloud",
			"app.user": "usr-123", "app.outcome": "ok",
		}))
		got := buf.String()
		if !strings.Contains(got, "app.user=usr-123") {
			t.Errorf("promoted app.user should render, got: %s", got)
		}
		if !strings.Contains(got, "app.outcome=ok") {
			t.Errorf("promoted app.outcome should render, got: %s", got)
		}
	})
}

func TestRenderLogEntry(t *testing.T) {
	mk := func(tsec int, line string) *app_v1alpha.LogEntry {
		e := &app_v1alpha.LogEntry{}
		e.SetTimestamp(standard.ToTimestamp(time.Date(2026, 3, 13, 16, 30, tsec, 0, time.UTC)))
		e.SetStream("stdout")
		e.SetLine(line)
		return e
	}

	t.Run("same content different timestamp shares signature", func(t *testing.T) {
		_, sig1 := renderLogEntry(mk(11, "ping"))
		d2, sig2 := renderLogEntry(mk(12, "ping"))
		d1, _ := renderLogEntry(mk(11, "ping"))

		if sig1 != sig2 {
			t.Errorf("signatures should match for lines differing only by timestamp: %q vs %q", sig1, sig2)
		}
		if d1 == d2 {
			t.Errorf("displays should differ when timestamps differ, both = %q", d1)
		}
	})

	t.Run("different content produces different signature", func(t *testing.T) {
		_, sig1 := renderLogEntry(mk(11, "ping"))
		_, sig2 := renderLogEntry(mk(11, "pong"))
		if sig1 == sig2 {
			t.Errorf("signatures should differ for different content, both = %q", sig1)
		}
	})
}

func TestLogCoalescer(t *testing.T) {
	mk := func(tsec int, line string) *app_v1alpha.LogEntry {
		e := &app_v1alpha.LogEntry{}
		e.SetTimestamp(standard.ToTimestamp(time.Date(2026, 3, 13, 16, 30, tsec, 0, time.UTC)))
		e.SetStream("stdout")
		e.SetLine(line)
		return e
	}

	// Construct the coalescer directly with a buffer-backed Context, bypassing
	// the TTY gate so the collapse logic itself is exercised.
	newCoalescer := func(buf *bytes.Buffer) *logCoalescer {
		ctx := &Context{Context: context.Background(), Stdout: buf}
		return &logCoalescer{ctx: ctx}
	}

	t.Run("repeated lines collapse to one live counter", func(t *testing.T) {
		var buf bytes.Buffer
		c := newCoalescer(&buf)
		for i := 0; i < 4; i++ {
			c.print(mk(11+i, "ping"))
		}
		c.flush()

		out := buf.String()
		// timestamps 11..14 → final count 4, span 3s
		if !strings.Contains(out, "[ Repeated 4x over 3s ]") {
			t.Errorf("expected summary [ Repeated 4x over 3s ], got: %q", out)
		}
		if !strings.Contains(out, "\r") {
			t.Errorf("expected at least one \\r redraw, got: %q", out)
		}
		// First occurrence committed once, then the summary redraws — exactly one
		// full "ping" line is followed by summary redraws, never four full lines.
		if strings.Count(out, "\n") != 2 {
			t.Errorf("expected 2 newlines (first line + flushed summary), got %d: %q", strings.Count(out, "\n"), out)
		}
	})

	t.Run("distinct line commits the counter and prints in full", func(t *testing.T) {
		var buf bytes.Buffer
		c := newCoalescer(&buf)
		c.print(mk(11, "ping"))
		c.print(mk(12, "ping"))
		c.print(mk(13, "different"))
		c.flush()

		out := buf.String()
		if !strings.Contains(out, "[ Repeated 2x over 1s ]") {
			t.Errorf("expected [ Repeated 2x over 1s ] for the ping run, got: %q", out)
		}
		if !strings.Contains(out, "different") {
			t.Errorf("expected the distinct line printed, got: %q", out)
		}
	})

	t.Run("same-second repeats omit the span", func(t *testing.T) {
		var buf bytes.Buffer
		c := newCoalescer(&buf)
		c.print(mk(11, "ping"))
		c.print(mk(11, "ping"))
		c.flush()

		out := buf.String()
		if !strings.Contains(out, "[ Repeated 2x ]") {
			t.Errorf("expected bare [ Repeated 2x ] with no span, got: %q", out)
		}
		if strings.Contains(out, "over") {
			t.Errorf("did not expect a span for a sub-second run, got: %q", out)
		}
	})

	t.Run("single non-repeated line has no summary", func(t *testing.T) {
		var buf bytes.Buffer
		c := newCoalescer(&buf)
		c.print(mk(11, "solo"))
		c.flush()

		out := buf.String()
		if strings.Contains(out, "Repeated") {
			t.Errorf("did not expect a summary for a single line, got: %q", out)
		}
		if strings.Contains(out, "\r") {
			t.Errorf("did not expect a \\r redraw for a single line, got: %q", out)
		}
		if !strings.Contains(out, "solo") {
			t.Errorf("expected the line content, got: %q", out)
		}
	})
}

func TestFormatRunSpan(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{1 * time.Second, "1s"},
		{14 * time.Second, "14s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},
		{75 * time.Second, "1m15s"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 5*time.Minute, "2h5m"},
	}
	for _, tt := range tests {
		if got := formatRunSpan(tt.d); got != tt.want {
			t.Errorf("formatRunSpan(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestRunSpanSuffix(t *testing.T) {
	base := time.Date(2026, 3, 13, 16, 30, 0, 0, time.UTC)

	if got := runSpanSuffix(base, base); got != "" {
		t.Errorf("zero span should produce no suffix, got %q", got)
	}
	if got := runSpanSuffix(base, base.Add(900*time.Millisecond)); got != "" {
		t.Errorf("sub-second span should produce no suffix, got %q", got)
	}
	if got := runSpanSuffix(base, base.Add(14*time.Second)); got != " over 14s" {
		t.Errorf("expected \" over 14s\", got %q", got)
	}
}

func TestLogPrinterNonTTYNeverCoalesces(t *testing.T) {
	// A bytes.Buffer Stdout is not a TTY, so even with follow=true the plain
	// printer must be returned — guaranteeing piped output keeps every line.
	var buf bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &buf}
	printer, flush := logPrinter(ctx, false, true)

	ts := time.Date(2026, 3, 13, 16, 30, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		e := &app_v1alpha.LogEntry{}
		e.SetTimestamp(standard.ToTimestamp(ts))
		e.SetStream("stdout")
		e.SetLine("ping")
		printer(e)
	}
	flush()

	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Errorf("non-TTY output must not use \\r redraws, got: %q", out)
	}
	if strings.Contains(out, "Repeated") {
		t.Errorf("non-TTY output must not collapse lines, got: %q", out)
	}
	if n := strings.Count(out, "ping"); n != 3 {
		t.Errorf("expected 3 verbatim ping lines, got %d: %q", n, out)
	}
}

func TestFormatAttributes(t *testing.T) {
	t.Run("filters hidden attributes", func(t *testing.T) {
		m := map[string]string{
			"miren.container": "app",
			"miren.sandbox":   "sandbox/test",
			"miren.service":   "web",
			"miren.stage":     "app-run",
			"miren.version":   "v1",
			"miren.short_id":  "abc",
			"source":          "test-id",
			"component":       "scheduler",
		}
		got := formatAttributes(m)
		if !strings.Contains(got, "component=scheduler") {
			t.Errorf("expected component=scheduler, got: %q", got)
		}
		if strings.Contains(got, "miren.") {
			t.Errorf("hidden attrs should not appear, got: %q", got)
		}
	})

	t.Run("returns empty when all attributes are hidden", func(t *testing.T) {
		m := map[string]string{
			"miren.container": "app",
			"miren.sandbox":   "sandbox/test",
		}
		got := formatAttributes(m)
		if got != "" {
			t.Errorf("expected empty, got: %q", got)
		}
	})
}

func TestParseTimeFlag(t *testing.T) {
	now := time.Date(2026, 6, 26, 15, 30, 0, 0, time.Local)

	t.Run("RFC3339 with zone", func(t *testing.T) {
		got, err := parseTimeFlag("2026-06-25T14:00:00Z", now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("naive datetime parsed as local", func(t *testing.T) {
		got, err := parseTimeFlag("2026-06-25 14:00", now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 6, 25, 14, 0, 0, 0, time.Local); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("date only is midnight local", func(t *testing.T) {
		got, err := parseTimeFlag("2026-06-25", now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 6, 25, 0, 0, 0, 0, time.Local); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("time only anchors to today", func(t *testing.T) {
		got, err := parseTimeFlag("14:30", now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 6, 26, 14, 30, 0, 0, time.Local); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("duration is treated as ago", func(t *testing.T) {
		got, err := parseTimeFlag("2h", now)
		if err != nil {
			t.Fatal(err)
		}
		if want := now.Add(-2 * time.Hour); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("surrounding whitespace tolerated", func(t *testing.T) {
		got, err := parseTimeFlag("  90m  ", now)
		if err != nil {
			t.Fatal(err)
		}
		if want := now.Add(-90 * time.Minute); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	for _, in := range []string{"", "not-a-time", "2026/06/25", "yesterday"} {
		t.Run("rejects "+in, func(t *testing.T) {
			if _, err := parseTimeFlag(in, now); err == nil {
				t.Errorf("parseTimeFlag(%q) = nil error, want error", in)
			}
		})
	}
}

func TestResolveLogWindow(t *testing.T) {
	now := time.Date(2026, 6, 26, 15, 30, 0, 0, time.Local)
	dur := 2 * time.Hour

	at := func(ts *standard.Timestamp) time.Time { return standard.FromTimestamp(ts) }

	t.Run("--since and --last conflict", func(t *testing.T) {
		if _, _, err := resolveLogWindow(&dur, "1h", "", false, now); err == nil {
			t.Error("expected error when both --since and --last set")
		}
	})

	t.Run("--until with --follow conflict", func(t *testing.T) {
		if _, _, err := resolveLogWindow(nil, "", "1h", true, now); err == nil {
			t.Error("expected error when --until combined with --follow")
		}
	})

	t.Run("--last sets from, until open", func(t *testing.T) {
		from, to, err := resolveLogWindow(&dur, "", "", false, now)
		if err != nil {
			t.Fatal(err)
		}
		if to != nil {
			t.Errorf("expected nil until, got %v", at(to))
		}
		if want := now.Add(-dur); !at(from).Equal(want) {
			t.Errorf("from = %v, want %v", at(from), want)
		}
	})

	t.Run("--since and --until set both bounds", func(t *testing.T) {
		from, to, err := resolveLogWindow(nil, "2026-06-25T14:00:00Z", "2026-06-25T15:00:00Z", false, now)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC); !at(from).Equal(want) {
			t.Errorf("from = %v, want %v", at(from), want)
		}
		if want := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC); !at(to).Equal(want) {
			t.Errorf("until = %v, want %v", at(to), want)
		}
	})

	t.Run("no flags leaves both open", func(t *testing.T) {
		from, to, err := resolveLogWindow(nil, "", "", false, now)
		if err != nil {
			t.Fatal(err)
		}
		if from != nil || to != nil {
			t.Errorf("expected both nil, got from=%v to=%v", from, to)
		}
	})

	t.Run("invalid --since surfaces error", func(t *testing.T) {
		if _, _, err := resolveLogWindow(nil, "bogus", "", false, now); err == nil {
			t.Error("expected error for invalid --since")
		}
	})

	t.Run("invalid --until surfaces error", func(t *testing.T) {
		if _, _, err := resolveLogWindow(nil, "", "bogus", false, now); err == nil {
			t.Error("expected error for invalid --until")
		}
	})

	t.Run("inverted window is rejected", func(t *testing.T) {
		// --since 1h, --until 2h → start (1h ago) is after end (2h ago).
		if _, _, err := resolveLogWindow(nil, "1h", "2h", false, now); err == nil {
			t.Error("expected error when --until is before --since")
		}
	})
}

func TestBuildSystemFilter(t *testing.T) {
	tests := []struct {
		name       string
		component  string
		userFilter string
		want       string
	}{
		{
			name: "no component, no filter",
			want: `source:"system"`,
		},
		{
			name:      "component only",
			component: "etcd",
			want:      `source:"system" module:"etcd"`,
		},
		{
			name:       "filter only",
			userFilter: "error",
			want:       `source:"system" error`,
		},
		{
			name:       "component and filter",
			component:  "scheduler",
			userFilter: "error",
			want:       `source:"system" module:"scheduler" error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSystemFilter(tt.component, tt.userFilter)
			if got != tt.want {
				t.Errorf("buildSystemFilter(%q, %q) = %q, want %q",
					tt.component, tt.userFilter, got, tt.want)
			}
		})
	}
}
