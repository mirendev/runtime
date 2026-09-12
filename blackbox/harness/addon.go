package harness

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// addonListEntry matches the JSON output of `miren addon list -a <app> --format json`.
type addonListEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Variant      string `json:"variant"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
}

// WaitForAddonReady polls `addon list` until the named addon is active on the
// app. An addon in error will never become active, so that fails the test at
// once with the controller's reason instead of running out the timeout.
func WaitForAddonReady(t *testing.T, m *Miren, appName, addonName string, timeout time.Duration) {
	t.Helper()

	Poll(t, fmt.Sprintf("addon %s ready on %s", addonName, appName), timeout, 3*time.Second, func() (bool, string) {
		r := m.Run("addon", "list", "-a", appName, "--format", "json")
		if !r.Success() {
			return false, fmt.Sprintf("addon list failed: %s", r.Stderr)
		}

		var addons []addonListEntry
		if err := json.Unmarshal([]byte(r.Stdout), &addons); err != nil {
			return false, fmt.Sprintf("failed to parse addon list JSON: %v", err)
		}

		for _, a := range addons {
			if a.Name != addonName {
				continue
			}
			switch a.Status {
			case "active":
				return true, ""
			case "error":
				t.Fatalf("addon %s on %s failed to provision: %s", addonName, appName, a.ErrorMessage)
			}
			return false, fmt.Sprintf("addon %s is %s", addonName, a.Status)
		}

		return false, fmt.Sprintf("addon %s not found in list (%d addons)", addonName, len(addons))
	})
}

// WaitForAddonRemoved polls `addon list` until the named addon no longer appears for the app.
func WaitForAddonRemoved(t *testing.T, m *Miren, appName, addonName string, timeout time.Duration) {
	t.Helper()

	Poll(t, fmt.Sprintf("addon %s removed from %s", addonName, appName), timeout, 3*time.Second, func() (bool, string) {
		r := m.Run("addon", "list", "-a", appName, "--format", "json")
		if !r.Success() {
			return false, fmt.Sprintf("addon list failed: %s", r.Stderr)
		}

		var addons []addonListEntry
		if err := json.Unmarshal([]byte(r.Stdout), &addons); err != nil {
			return false, fmt.Sprintf("failed to parse addon list JSON: %v", err)
		}

		for _, a := range addons {
			if a.Name == addonName {
				return false, fmt.Sprintf("addon %s still present", addonName)
			}
		}

		return true, ""
	})
}

// WaitForEnvVarRemoved polls `env list` until the given key no longer appears
// in the app's environment.
func WaitForEnvVarRemoved(t *testing.T, m *Miren, appName, key string, timeout time.Duration) {
	t.Helper()

	Poll(t, fmt.Sprintf("env var %s removed from %s", key, appName), timeout, 3*time.Second, func() (bool, string) {
		r := m.Run("env", "list", "-a", appName)
		if !r.Success() {
			return false, fmt.Sprintf("env list failed: %s", r.Stderr)
		}

		if r.OutputContains(key) {
			return false, fmt.Sprintf("%s still in env", key)
		}

		return true, ""
	})
}

// WaitForEnvVar polls `env list` until the given key appears in the app's environment.
func WaitForEnvVar(t *testing.T, m *Miren, appName, key string, timeout time.Duration) {
	t.Helper()

	Poll(t, fmt.Sprintf("env var %s on %s", key, appName), timeout, 3*time.Second, func() (bool, string) {
		r := m.Run("env", "list", "-a", appName)
		if !r.Success() {
			return false, fmt.Sprintf("env list failed: %s", r.Stderr)
		}

		if !r.OutputContains(key) {
			return false, fmt.Sprintf("%s not yet in env", key)
		}

		return true, ""
	})
}
