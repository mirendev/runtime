package harness

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runID is a process-unique identifier so that app names don't collide across
// concurrent or back-to-back test runs against the same cluster.
var runID = func() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}()

// AppOptions configures a test app deployment.
type AppOptions struct {
	// Name overrides the generated app name. If empty, one is derived from the
	// test name and Testdata value.
	Name string

	// Testdata is the directory name under testdata/ to deploy (e.g. "go-server").
	Testdata string

	// Env is a list of KEY=VALUE pairs to pass via -e flags.
	Env []string

	// ReadyTimeout is how long to wait for the app to become healthy after deploy.
	// Defaults to 2 minutes.
	ReadyTimeout time.Duration
}

// UniqueAppName generates a short app name unique to this test and process. The
// format is bb-<base>-<hash> where hash is 6 hex chars derived from the test
// name plus a process-unique run ID to avoid collisions across concurrent runs.
func UniqueAppName(t *testing.T, base string) string {
	t.Helper()
	h := sha256.Sum256([]byte(t.Name() + "/" + runID))
	return fmt.Sprintf("bb-%s-%.6x", base, h[:3])
}

// DeployApp deploys a testdata application, registers cleanup, and waits for
// it to become ready. It returns the app name.
func DeployApp(t *testing.T, m *Miren, opts AppOptions) string {
	t.Helper()

	if opts.Testdata == "" {
		t.Fatal("AppOptions.Testdata is required")
	}

	name := opts.Name
	if name == "" {
		name = UniqueAppName(t, opts.Testdata)
	}

	readyTimeout := opts.ReadyTimeout
	if readyTimeout == 0 {
		readyTimeout = 2 * time.Minute
	}

	// Build the deploy command
	hostDir := filepath.Join(m.cluster.TestdataDir, opts.Testdata)
	containerDir := m.ContainerPath(hostDir)

	args := []string{"deploy", "-a", name, "-d", containerDir, "-f"}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}

	// Register cleanup before deploying so it runs even if deploy fails partway
	t.Cleanup(func() {
		t.Logf("cleaning up app %s", name)
		if r := m.Run("app", "delete", name, "-f"); !r.Success() {
			t.Errorf("failed to delete app %s during cleanup: %s", name, strings.TrimSpace(r.Stderr))
		}
	})

	m.MustRun(args...)

	WaitForAppReady(t, m, name, readyTimeout)

	return name
}

// GetSandboxID returns the ID of a running sandbox for the given app by parsing
// the JSON output of `sandbox list --format json`.
func GetSandboxID(t *testing.T, m *Miren, appName string) string {
	t.Helper()

	r := m.MustRun("sandbox", "list", "--format", "json")

	var sandboxes []struct {
		ID   string `json:"id"`
		Spec struct {
			Version string `json:"version"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &sandboxes); err != nil {
		t.Fatalf("failed to parse sandbox list JSON: %v", err)
	}

	for _, sb := range sandboxes {
		// Sandbox IDs and versions contain the app name
		if strings.Contains(sb.ID, appName) || strings.Contains(sb.Spec.Version, appName) {
			return sb.ID
		}
	}

	t.Fatalf("no sandbox found for app %s", appName)
	return ""
}

// appListEntry matches the JSON output of `miren app list --format json`.
type appListEntry struct {
	Name           string `json:"name"`
	Health         string `json:"health"`
	ReadyInstances int    `json:"ready_instances"`
}

// WaitForAppReady polls `app list --json` until the named app reports healthy.
func WaitForAppReady(t *testing.T, m *Miren, name string, timeout time.Duration) {
	t.Helper()

	Poll(t, fmt.Sprintf("app %s ready", name), timeout, 3*time.Second, func() (bool, string) {
		r := m.Run("app", "list", "--format", "json")
		if !r.Success() {
			return false, fmt.Sprintf("app list failed: %s", r.Stderr)
		}

		var apps []appListEntry
		if err := json.Unmarshal([]byte(r.Stdout), &apps); err != nil {
			return false, fmt.Sprintf("failed to parse app list JSON: %v", err)
		}

		for _, app := range apps {
			if app.Name == name {
				switch app.Health {
				// "ready" is a task-only app: deployed and invokable, with no
				// long-running process to become healthy.
				case "healthy", "ready":
					return true, ""
				case "crashed":
					return false, fmt.Sprintf("app %s health: crashed (may recover after env injection)", name)
				default:
					return false, fmt.Sprintf("app %s health: %s (ready: %d)", name, app.Health, app.ReadyInstances)
				}
			}
		}

		return false, fmt.Sprintf("app %s not found in app list", name)
	})
}
