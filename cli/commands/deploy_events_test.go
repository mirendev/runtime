package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"
)

// decodeEvents parses a JSONL buffer, failing the test on any line that is not
// a JSON object, and returns each line as a generic map.
func decodeEvents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%q", i+1, err, line)
		}
		if _, ok := ev["event"]; !ok {
			t.Fatalf("line %d has no event field: %s", i+1, line)
		}
		if _, ok := ev["time"]; !ok {
			t.Fatalf("line %d has no time field: %s", i+1, line)
		}
		events = append(events, ev)
	}
	return events
}

func TestDeployEventStream_BuildStepsAndLogs(t *testing.T) {
	var buf bytes.Buffer
	s := newDeployEventStream(&buf)

	now := time.Now()
	d := digest.FromString("step-a")
	started := &client.Vertex{Digest: d, Name: "[phase] Compiling", Started: &now}
	done := &client.Vertex{Digest: d, Name: "[phase] Compiling", Started: &now, Completed: &now}
	cached := &client.Vertex{Digest: digest.FromString("step-b"), Name: "copy /app", Cached: true, Completed: &now}

	s.observeSolveStatus(&client.SolveStatus{Vertexes: []*client.Vertex{started}})
	// The same state again must not repeat the event.
	s.observeSolveStatus(&client.SolveStatus{Vertexes: []*client.Vertex{started}})
	s.observeSolveStatus(&client.SolveStatus{
		Vertexes: []*client.Vertex{done, cached},
		Logs:     []*client.VertexLog{{Vertex: d, Data: []byte("line one\nline two\n")}},
	})

	events := decodeEvents(t, buf.String())
	var got []string
	for _, ev := range events {
		switch ev["event"] {
		case "build_step":
			got = append(got, ev["step"].(string)+"="+ev["status"].(string))
		case "build_log":
			got = append(got, "log:"+ev["line"].(string))
		}
	}
	want := []string{
		"[phase] Compiling=started",
		"[phase] Compiling=done",
		"copy /app=cached",
		"log:line one",
		"log:line two",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %v\nwant     %v", got, want)
	}
}

func TestDeployEventStream_ResultCarriesSummary(t *testing.T) {
	var buf bytes.Buffer
	s := newDeployEventStream(&buf)

	sum := &deploySummary{App: "meet", Cluster: "prod", DeployID: "dpl_1", AppVersion: "meet-v1"}
	sum.finalize(errors.New("boom"))
	s.result(sum)

	events := decodeEvents(t, buf.String())
	if len(events) != 1 {
		t.Fatalf("expected one line, got %d", len(events))
	}
	ev := events[0]
	if ev["event"] != "result" || ev["status"] != "failed" || ev["error"] != "boom" || ev["app_version"] != "meet-v1" {
		t.Fatalf("result event = %v", ev)
	}
	// The first key is "event" so the stream is skimmable by eye.
	if !strings.HasPrefix(buf.String(), `{"event":"result"`) {
		t.Fatalf("event must be the first key: %s", buf.String())
	}
}

func TestDeployEventStream_HealthObserver(t *testing.T) {
	var buf bytes.Buffer
	s := newDeployEventStream(&buf)
	var obs healthObserver = s

	obs.healthWaiting("meet-v1")
	obs.healthVerdict("meet-v1", "Version v1 is live and serving", true, 1500*time.Millisecond)
	obs.healthPortWarning(8080, "0.0.0.0")
	obs.healthAppLog("panic: oh no")

	events := decodeEvents(t, buf.String())
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %s", len(events), buf.String())
	}
	if events[0]["status"] != "waiting" || events[1]["status"] != "healthy" || events[1]["duration_ms"] != float64(1500) {
		t.Fatalf("health events = %v %v", events[0], events[1])
	}
	if events[2]["event"] != "port_warning" || events[2]["port"] != float64(8080) {
		t.Fatalf("port warning = %v", events[2])
	}
	if events[3]["event"] != "app_log" || events[3]["line"] != "panic: oh no" {
		t.Fatalf("app log = %v", events[3])
	}
}

// TestDeploy_JSONLIsTheOnlyOutput drives Deploy through its earliest failure
// and checks the contract: stdout is nothing but JSON lines, ending in a
// result, stderr gets nothing at all, and the CLI is handed a bare exit code so
// it prints nothing either.
func TestDeploy_JSONLIsTheOnlyOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := &Context{
		Context: context.Background(),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	opts := deployOpts{FormatOptions: FormatOptions{Format: "jsonl"}}
	opts.App = "meet"

	err := Deploy(ctx, opts)
	var exitErr ErrExitCode
	if !errors.As(err, &exitErr) || int(exitErr) != 1 {
		t.Fatalf("expected ErrExitCode(1) so the CLI stays silent, got %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr must be empty in jsonl mode, got %q", stderr.String())
	}

	events := decodeEvents(t, stdout.String())
	if events[0]["event"] != "start" || events[0]["app"] != "meet" {
		t.Fatalf("first event = %v, want start", events[0])
	}
	last := events[len(events)-1]
	if last["event"] != "result" || last["status"] != "failed" || last["error"] == "" {
		t.Fatalf("last event = %v, want failed result with error", last)
	}
	if ctx.Stdout != &stdout {
		t.Fatal("ctx.Stdout was not restored after the deploy")
	}
}
