//go:build blackbox

package blackbox

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestDeployScriptableOutput covers what a CI job or script sees: the harness
// captures stdout through a pipe, so this is the non-TTY path.
func TestDeployScriptableOutput(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "go-server")
	containerDir := m.ContainerPath(filepath.Join(c.TestdataDir, "go-server"))
	t.Cleanup(func() {
		if r := m.Run("app", "delete", name, "-f"); !r.Success() {
			t.Errorf("failed to delete app %s during cleanup: %s", name, strings.TrimSpace(r.Stderr))
		}
	})

	t.Run("plain text ends with a status marker and a Version line", func(t *testing.T) {
		r := m.MustRun("deploy", "-a", name, "-d", containerDir, "-f")

		if strings.Contains(r.Stdout, "\x1b") || strings.Contains(r.Stderr, "\x1b[") {
			t.Errorf("non-TTY deploy output must not contain escape sequences:\nstdout: %q\nstderr: %q", r.Stdout, r.Stderr)
		}
		if !strings.Contains(r.Stdout, "✓ Deploy successful") {
			t.Errorf("stdout missing success marker:\n%s", r.Stdout)
		}
		if versionLine(r.Stdout) == "" {
			t.Errorf("stdout missing a 'Version: <id>' line:\n%s", r.Stdout)
		}
		// Build steps are condensed to one summary line rather than replayed.
		if !strings.Contains(r.Stdout, "Build & push image") {
			t.Errorf("stdout missing build summary line:\n%s", r.Stdout)
		}
	})

	t.Run("json puts one document on stdout", func(t *testing.T) {
		r := m.MustRun("deploy", "-a", name, "-d", containerDir, "--format", "json")

		var doc struct {
			Status     string   `json:"status"`
			App        string   `json:"app"`
			DeployID   string   `json:"deploy_id"`
			AppVersion string   `json:"app_version"`
			URLs       []string `json:"urls"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil {
			t.Fatalf("stdout has to be the result document and nothing else: %v\nstdout: %s", err, r.Stdout)
		}
		// The spelling is the deployment record's own, the one `app history`
		// reports for the same deploy_id.
		if doc.Status != "succeeded" {
			t.Errorf("status = %q, want succeeded", doc.Status)
		}
		if doc.App != name {
			t.Errorf("app = %q, want %q", doc.App, name)
		}
		if doc.AppVersion == "" || doc.DeployID == "" {
			t.Errorf("app_version %q and deploy_id %q must both be set", doc.AppVersion, doc.DeployID)
		}
		if doc.URLs == nil {
			t.Error("urls must be an array, not null")
		}
		// The running commentary still exists, it just lives on stderr.
		if !strings.Contains(r.Stderr, "Deploy successful") {
			t.Errorf("expected progress text on stderr, got:\n%s", r.Stderr)
		}
	})

	t.Run("jsonl streams events and nothing else", func(t *testing.T) {
		r := m.MustRun("deploy", "-a", name, "-d", containerDir, "--format", "jsonl")

		if strings.TrimSpace(r.Stderr) != "" {
			t.Errorf("jsonl mode must write nothing to stderr, got:\n%s", r.Stderr)
		}

		lines := strings.Split(strings.TrimRight(r.Stdout, "\n"), "\n")
		seen := map[string]int{}
		var last map[string]any
		for i, line := range lines {
			var ev map[string]any
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("stdout line %d is not JSON: %v\n%s", i+1, err, line)
			}
			name, _ := ev["event"].(string)
			seen[name]++
			last = ev
		}
		if first, _ := lines[0], seen["start"]; !strings.HasPrefix(first, `{"event":"start"`) {
			t.Errorf("first line must be the start event, got %s", first)
		}
		for _, want := range []string{"upload_complete", "build_step", "build_complete", "deployment", "health", "result"} {
			if seen[want] == 0 {
				t.Errorf("no %q event in stream:\n%s", want, r.Stdout)
			}
		}
		if last["event"] != "result" || last["status"] != "succeeded" || last["app_version"] == "" {
			t.Errorf("last line must be a successful result, got %v", last)
		}
	})

	t.Run("quiet keeps summaries and drops progress", func(t *testing.T) {
		r := m.MustRun("deploy", "-a", name, "-d", containerDir, "-f", "-q")

		if !strings.Contains(r.Stdout, "✓ Deploy successful") || !strings.Contains(r.Stdout, "Build & push image") {
			t.Errorf("quiet stdout must still carry the phase summary and status:\n%s", r.Stdout)
		}
		// BuildKit's step-by-step stream ("#1 [internal] ...", "#2 DONE") is
		// what --quiet exists to drop.
		if strings.Contains(r.Stderr, "\n#1 ") || strings.Contains(r.Stderr, "DONE") {
			t.Errorf("quiet stderr still carries build step output:\n%s", r.Stderr)
		}
		if strings.Contains(r.Stderr, "Uploading artifacts:") {
			t.Errorf("quiet stderr still carries upload progress:\n%s", r.Stderr)
		}
	})
}

// versionLine returns the value of the first "Version: <id>" line in out.
func versionLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "Version: "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func TestDeployGoServer(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Verify it shows up in app list
	r := m.MustRun("app", "list", "--format", "json")
	r.RequireContains(t, name)

	// Verify logs are flowing
	harness.Poll(t, "logs available", 30*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("logs", "-a", name)
			if r.OutputContains("starting on port") || r.OutputContains("Server starting") {
				return true, ""
			}
			return false, "no startup log yet"
		},
	)
}
