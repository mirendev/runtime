package commands

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleDeployEvents = `{"event":"start","time":"2026-09-10T18:29:59.377Z","app":"hw-bun","cluster":"disttest4"}
{"event":"message","time":"2026-09-10T18:30:00.000Z","message":"Receiving changed files"}
{"event":"upload","time":"2026-09-10T18:30:00.500Z","bytes":2048,"bytes_per_second":1024,"fraction":0.5}
{"event":"upload_complete","time":"2026-09-10T18:30:01.377Z","bytes":4096,"duration_ms":2000,"reused_files":3,"total_files":4,"saved_bytes":0}
{"event":"build_step","time":"2026-09-10T18:30:01.400Z","step":"[phase] Compiling","digest":"sha256:aaa","status":"started"}
{"event":"build_log","time":"2026-09-10T18:30:01.500Z","step":"[phase] Compiling","digest":"sha256:aaa","line":"go build ./..."}
{"event":"build_step","time":"2026-09-10T18:30:02.000Z","step":"[phase] Compiling","digest":"sha256:aaa","status":"done"}
{"event":"build_step","time":"2026-09-10T18:30:02.100Z","step":"copy /app","digest":"sha256:bbb","status":"cached"}
{"event":"build_complete","time":"2026-09-10T18:30:02.200Z","steps":2,"cached":1,"duration_ms":800}
{"event":"deployment","time":"2026-09-10T18:30:02.300Z","deploy_id":"deployment-1","phase":"activating"}
{"event":"log","time":"2026-09-10T18:30:02.374Z","level":"ERROR","message":"rpc.callstream: error calling inline","fields":{"error":"boom"}}
{"event":"health","time":"2026-09-10T18:30:02.400Z","version":"hw-bun-v1","status":"waiting"}
{"event":"health","time":"2026-09-10T18:30:06.400Z","version":"hw-bun-v1","status":"healthy","message":"Version v1 is live and serving","duration_ms":4000}
this line is not json
{"event":"result","time":"2026-09-10T18:30:06.500Z","status":"success","app":"hw-bun","cluster":"disttest4","deploy_id":"deployment-1","app_version":"hw-bun-v1","urls":["https://hw-bun.example.com"]}
`

func renderSample(t *testing.T, timestamps, buildLogs bool) string {
	t.Helper()
	var out bytes.Buffer
	ctx := &Context{
		Context: context.Background(),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stdout:  &out,
		Stderr:  io.Discard,
	}
	r := newDeployEventRenderer(ctx, timestamps, buildLogs)
	for _, line := range strings.Split(sampleDeployEvents, "\n") {
		r.renderLine([]byte(line))
	}
	return out.String()
}

func TestDebugDeployEvents_RendersReadableStream(t *testing.T) {
	got := renderSample(t, false, false)

	for _, want := range []string{
		"Deploying: hw-bun → disttest4",
		"Receiving changed files",
		"Uploading artifacts: 50% — 2.0 KB at 1.0 KB/s",
		"Upload artifacts",
		"reused 3/4 files",
		"#1 [phase] Compiling",
		"#1 DONE",
		"#2 copy /app",
		"#2 CACHED",
		"Build & push image",
		"2 steps, 1 cached",
		"deployment deployment-1: activating",
		"[ERROR] rpc.callstream: error calling inline error=boom",
		"Waiting for version hw-bun-v1 to become healthy...",
		"✓ Version v1 is live and serving",
		"this line is not json",
		"Your app is available at:",
		"  https://hw-bun.example.com",
		"✓ Deploy successful",
		"Version: hw-bun-v1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "go build ./...") {
		t.Errorf("build log lines must be hidden by default:\n%s", got)
	}
}

func TestDebugDeployEvents_BuildLogsAndTimestamps(t *testing.T) {
	got := renderSample(t, true, true)

	if !strings.Contains(got, "#1 go build ./...") {
		t.Errorf("--build-logs must show step output:\n%s", got)
	}
	// Elapsed time since the start event, which was at 18:29:59.377.
	if !strings.Contains(got, "+  0.000s") || !strings.Contains(got, "+  7.023s") {
		t.Errorf("timestamps must be elapsed since the first event:\n%s", got)
	}
	// Spacer lines stay blank rather than carrying a stamp on nothing.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "+") && strings.HasSuffix(line, "s ") {
			t.Errorf("blank spacer line carries a stamp: %q", line)
		}
	}
	if !strings.Contains(got, "\n\n") {
		t.Errorf("expected blank spacer lines in:\n%s", got)
	}
}

func TestDebugDeployEvents_FailedResult(t *testing.T) {
	var out bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &out, Stderr: io.Discard}
	r := newDeployEventRenderer(ctx, false, false)
	r.renderLine([]byte(`{"event":"build_error","time":"2026-09-10T18:30:02.456Z","message":"Error extracting changed files"}`))
	r.renderLine([]byte(`{"event":"result","time":"2026-09-10T18:30:02.679Z","status":"failed","app":"hw-bun","deploy_id":"","app_version":"","urls":[],"error":"archive/tar: write too long"}`))

	got := out.String()
	for _, want := range []string{"✗ Error extracting changed files", "✗ Deploy failed", "Error: archive/tar: write too long"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Version:") {
		t.Errorf("no Version line without a version:\n%s", got)
	}
}

func TestDebugDeployEvents_VersionIsCleanedLikeLiveOutput(t *testing.T) {
	var out bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &out, Stderr: io.Discard}
	r := newDeployEventRenderer(ctx, false, false)
	r.renderLine([]byte(`{"event":"result","time":"2026-09-10T18:30:06.500Z","status":"success","app_version":"app_version/hw-bun-v1","urls":[]}`))
	if !strings.Contains(out.String(), "Version: hw-bun-v1\n") {
		t.Fatalf("entity prefix must be stripped:\n%s", out.String())
	}
}

func TestDebugDeployEvents_UnknownEventEchoesRawLine(t *testing.T) {
	var out bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &out, Stderr: io.Discard}
	r := newDeployEventRenderer(ctx, false, false)
	raw := `{"event":"future_thing","time":"2026-09-10T18:30:06.500Z","novel_field":42}`
	r.renderLine([]byte(raw))
	got := out.String()
	if !strings.Contains(got, raw) {
		t.Fatalf("unknown event must echo the original line:\n%s", got)
	}
	if strings.Contains(got, `"app":""`) {
		t.Fatalf("unknown event must not print re-marshalled empty fields:\n%s", got)
	}
}

func TestDebugDeployEvents_ReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.jsonl")
	if err := os.WriteFile(path, []byte(sampleDeployEvents), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &out, Stderr: io.Discard}
	err := DebugDeployEvents(ctx, struct {
		File       string `position:"0" usage:"JSONL file written by 'miren deploy --format jsonl' (default: stdin)"`
		Timestamps bool   `short:"t" long:"timestamps" description:"Prefix each line with the time elapsed since the first event"`
		BuildLogs  bool   `long:"build-logs" description:"Show the log lines of every build step, not just the steps"`
	}{File: path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "✓ Deploy successful") {
		t.Fatalf("file input not rendered:\n%s", out.String())
	}
}
