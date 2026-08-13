//go:build blackbox

package blackbox

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestRPCViaCloud exercises the whole path an operator with no route to their
// cluster takes: the CLI dials cloud, cloud relays into the socket the cluster
// dialed outbound, and the cluster answers.
//
//	miren → cloud relay → cluster's uplink → cluster's RPC server
//
// The assertion that matters is not that a command succeeds but that it returns
// the same thing the direct path does. A relay that quietly answered from
// somewhere else would still exit zero.
//
// The RBAC grant in the setup is load-bearing evidence, not boilerplate:
// without it the cluster refuses these calls with its own policy decision,
// which is only reachable if it authenticated the caller's bearer and read the
// groups out of it. Cloud never sees that token — it is inside a CBOR frame —
// so nothing here could be cloud vouching for the traffic it carries.
func TestRPCViaCloud(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	env := harness.NewCloudEnv(t, m)

	// Something to look at that is real cluster state rather than a constant
	// any endpoint could produce.
	appName := harness.DeployApp(t, m, harness.AppOptions{Testdata: "go-server"})

	direct := m.MustRun("app", "list", "--format", "json")

	configPath, devUserXID := env.SetupViaCloudCluster(t, "viacloud")
	m.SetEnv("MIREN_CONFIG", configPath)
	t.Cleanup(func() { m.SetEnv("MIREN_CONFIG", "") })

	directApps := appNames(t, direct.Stdout)

	// The cluster caches the RBAC policy it fetched at startup and only
	// re-reads it once a denial tells it something may have changed, so the
	// grant made a moment ago takes a beat to be visible. Polling waits that
	// out; it does not paper over a relay that never works.
	var relayedApps []string
	harness.Poll(t, "app list via cloud", 90*time.Second, 3*time.Second, func() (bool, string) {
		r := m.Run("app", "list", "--format", "json")
		if !r.Success() {
			return false, "exit " + strconv.Itoa(r.ExitCode) + ": " + r.Stderr
		}
		relayedApps = appNames(t, r.Stdout)
		return true, ""
	})

	if len(relayedApps) == 0 {
		t.Fatalf("relayed app list returned nothing; direct returned %v", directApps)
	}
	if !slices.Equal(directApps, relayedApps) {
		t.Fatalf("relayed app list disagrees with the direct one:\n  direct:  %v\n  relayed: %v",
			directApps, relayedApps)
	}
	if !slices.Contains(relayedApps, appName) {
		t.Fatalf("relayed app list is missing the app just deployed (%s): %v", appName, relayedApps)
	}

	// A second command on a fresh connection: the relay has to be usable more
	// than once, not just for whatever session the first call happened to open.
	m.MustRun("app", "list", "--format", "json")

	// whoami reads no address of its own, and a cloud-routed cluster has none
	// to give it. Commands that reach for the hostname directly are the ones
	// this routing breaks, so one of them is exercised here.
	var who struct {
		Cluster string `json:"cluster"`
		UserID  string `json:"user_id"`
	}
	out := m.MustRun("whoami", "--format", "json").Stdout
	if err := json.Unmarshal([]byte(out), &who); err != nil {
		t.Fatalf("failed to parse whoami json: %v\nraw: %s", err, out)
	}
	if who.UserID != devUserXID {
		t.Fatalf("whoami reports user %q, want %q", who.UserID, devUserXID)
	}
}

func appNames(t *testing.T, out string) []string {
	t.Helper()

	var apps []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &apps); err != nil {
		t.Fatalf("failed to parse app list json: %v\nraw: %s", err, out)
	}

	names := make([]string, 0, len(apps))
	for _, a := range apps {
		names = append(names, a.Name)
	}

	// Sorted so the comparison is about which apps came back, not the order
	// two independent list calls happened to produce them in.
	slices.Sort(names)

	return names
}
