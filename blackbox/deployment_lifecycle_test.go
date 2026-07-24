//go:build blackbox

package blackbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// deploymentHistory mirrors the JSON shape of `miren app history --format json`.
type deploymentHistory struct {
	App         string                  `json:"app"`
	Cluster     string                  `json:"cluster"`
	Deployments []deploymentHistoryItem `json:"deployments"`
}

type deploymentHistoryItem struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	AppVersionID string `json:"app_version_id,omitempty"`
	Phase        string `json:"phase,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func appHistory(t *testing.T, m *harness.Miren, name string) []deploymentHistoryItem {
	t.Helper()
	r := m.MustRun("app", "history", "-a", name, "--format", "json")
	var out deploymentHistory
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("failed to parse app history: %v\noutput: %s", err, r.Stdout)
	}
	return out.Deployments
}

// A successful server-owned deploy must leave exactly one active record whose
// app_version_id is a real version — never the "pending-build" placeholder the
// old client used to write.
func TestDeployHistoryRecordsActiveVersion(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "go-server")
	t.Cleanup(func() { m.Run("app", "delete", name, "-f") })

	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/go-server"), "-f")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	deployments := appHistory(t, m, name)
	if len(deployments) == 0 {
		t.Fatal("expected at least one deployment in history")
	}

	// Newest first.
	latest := deployments[0]
	if latest.Status != "active" {
		t.Fatalf("expected latest deployment active, got %q", latest.Status)
	}
	if latest.AppVersionID == "" {
		t.Fatal("active deployment must carry a real app version id")
	}
	if latest.AppVersionID == "pending-build" || strings.HasPrefix(latest.AppVersionID, "failed-") {
		t.Fatalf("history leaked a placeholder version: %q", latest.AppVersionID)
	}
}

// A build that fails during compilation must leave a failed record with the
// error captured, and must not surface a placeholder version in history.
func TestFailedBuildLeavesFailedRecord(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "build-error")
	t.Cleanup(func() { m.Run("app", "delete", name, "-f") })

	// The build-error testdata does not compile, so the deploy fails.
	m.Run("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/build-error"), "-f").
		RequireExitCode(t, 1)

	// The server owns the record, so the failure must be visible in history
	// without the client having written anything.
	harness.Poll(t, "failed deployment recorded", 90*time.Second, 3*time.Second,
		func() (bool, string) {
			for _, d := range appHistory(t, m, name) {
				if d.Status == "failed" {
					if d.AppVersionID == "pending-build" || strings.HasPrefix(d.AppVersionID, "failed-") {
						return false, "record still carries a placeholder version"
					}
					if d.ErrorMessage == "" {
						return false, "failed record has no error message"
					}
					return true, ""
				}
			}
			return false, "no failed deployment yet"
		},
	)
}

// A second deploy of the same app+cluster, started while the first is still in
// flight, must be blocked by the deploy lock rather than run alongside it.
//
// The slow-build testdata sleeps during its build, giving a wide, deterministic
// window in which the first deploy holds the lock. (On a warm local cache the
// build layer may be reused and the window shrinks; CI runs on a cold cache.)
func TestConcurrentDeployBlockedByLock(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.UniqueAppName(t, "slow-build")
	t.Cleanup(func() { m.Run("app", "delete", name, "-f") })

	dir := m.ContainerPath(c.TestdataDir + "/slow-build")

	// First deploy runs in the background so we can race a second against it.
	first := m.RunCmdBackground(t, nil, "m", "deploy", "-a", name, "-d", dir, "-f")
	t.Cleanup(func() { first.Stop() })

	// Wait until the first deploy has taken the lock (an in-progress record).
	harness.Poll(t, "first deploy in progress", 60*time.Second, 500*time.Millisecond,
		func() (bool, string) {
			for _, d := range appHistory(t, m, name) {
				if d.Status == "in_progress" {
					return true, ""
				}
			}
			return false, "no in-progress deployment yet"
		},
	)

	// A second concurrent deploy must be refused while the lock is held. The
	// slow build keeps the first deploy in flight for ~25s, so this races
	// comfortably inside the window.
	second := m.Run("deploy", "-a", name, "-d", dir, "-f")
	if second.Success() {
		t.Fatal("expected the second concurrent deploy to be blocked, but it succeeded")
	}
	if !second.OutputContains("blocked") {
		t.Fatalf("expected a 'blocked' message, got:\nstdout: %s\nstderr: %s", second.Stdout, second.Stderr)
	}
}
