//go:build blackbox

package blackbox

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestSqliteAddonReplication covers the way most apps ask for a database:
// declaring the miren-sqlite addon. The addon contributes the disk to the app's
// services, so this also exercises an addon reaching into the app's own sandbox
// rather than standing up a server of its own.
func TestSqliteAddonReplication(t *testing.T) {
	runSqliteReplicationTest(t, sqliteCase{
		testdata: "sqlite-addon-app",
		addon:    "miren-sqlite",
		// The addon names the database itself, so storage is keyed on its
		// default identity rather than anything in app.toml.
		wantKeyFragment: "default",
	})
}

type sqliteCase struct {
	testdata        string
	addon           string
	wantKeyFragment string
}

// runSqliteReplicationTest checks what both declarations must deliver: the
// runtime hands the app a WAL-mode database, what it writes reaches the
// coordinator, and the data survives the sandbox being replaced.
func runSqliteReplicationTest(t *testing.T, tc sqliteCase) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := deploySqliteApp(t, m, c, tc)

	host := name + ".test.local"
	m.MustRun("route", "set", host, name)
	t.Cleanup(func() {
		m.Run("route", "remove", host)
	})

	harness.Poll(t, "HTTP reachable", 60*time.Second, 2*time.Second, func() (bool, string) {
		code, _, err := harness.HTTPGet(m, host, "/health")
		if err != nil {
			return false, fmt.Sprintf("HTTP error: %v", err)
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP status %d", code)
		}
		return true, ""
	})

	// litestream can only replicate a database in WAL mode, so the runtime must
	// have created it that way before the app ever opened it.
	code, mode, err := harness.HTTPGet(m, host, "/journal-mode")
	if err != nil {
		t.Fatalf("failed to read journal mode: %v", err)
	}
	if code != 200 || !strings.EqualFold(strings.TrimSpace(mode), "wal") {
		t.Fatalf("expected WAL journal mode, got %d/%q", code, mode)
	}

	postNote(t, m, host, "first")
	postNote(t, m, host, "second")
	requireNotes(t, m, host, "first\nsecond")

	// Replication is what distinguishes this from a plain local disk. The
	// coordinator's store lives on the host filesystem, which only the dev
	// topology lets us read; everything else here holds in any topology.
	if c.Mode == harness.ModeDev {
		harness.Poll(t, "ltx files replicated to coordinator", 60*time.Second, 2*time.Second, func() (bool, string) {
			if ltxFileListing(t, m) == "" {
				return false, "no .ltx files under /var/lib/miren/sqlite-backups yet"
			}
			return true, ""
		})

		listing := ltxFileListing(t, m)
		t.Logf("replicated files:\n%s", listing)
		if !strings.Contains(listing, tc.wantKeyFragment) {
			t.Fatalf("expected backup key to contain %q, got:\n%s", tc.wantKeyFragment, listing)
		}
		if !strings.Contains(listing, name) {
			t.Fatalf("expected backup key to be namespaced by app %q, got:\n%s", name, listing)
		}
	} else {
		t.Log("skipping backup-store inspection: requires dev topology")
	}

	// A redeploy replaces the sandbox, which detaches and reattaches the disk.
	m.MustRun("env", "set", "-a", name, "-e", "REDEPLOY_MARKER=v2")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	harness.Poll(t, "new version HTTP reachable", 60*time.Second, 2*time.Second, func() (bool, string) {
		code, _, err := harness.HTTPGet(m, host, "/health")
		if err != nil {
			return false, fmt.Sprintf("HTTP error: %v", err)
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP status %d", code)
		}
		return true, ""
	})

	requireNotes(t, m, host, "first\nsecond")

	// Writes after the reattach must replicate too, proving replication
	// restarted rather than silently stopping when the disk was re-registered.
	postNote(t, m, host, "third")
	requireNotes(t, m, host, "first\nsecond\nthird")
}

// deploySqliteApp deploys the case's app and returns its name.
//
// An app that declares an addon cannot use DeployApp: that waits for the exact
// version it deployed to become active, and provisioning replaces that version
// with one carrying the addon's contribution. The app also cannot be healthy
// until the database is attached. So deploy without waiting, let the addon
// finish, and only then wait for the app — the same sequence the Postgres addon
// test uses, and for the same reason.
func deploySqliteApp(t *testing.T, m *harness.Miren, c *harness.Cluster, tc sqliteCase) string {
	t.Helper()

	if tc.addon == "" {
		return harness.DeployApp(t, m, harness.AppOptions{Testdata: tc.testdata})
	}

	name := harness.UniqueAppName(t, tc.testdata)
	t.Cleanup(func() {
		t.Logf("cleaning up app %s", name)
		m.Run("app", "delete", name, "-f")
	})

	containerDir := m.ContainerPath(filepath.Join(c.TestdataDir, tc.testdata))
	m.MustRun("deploy", "-a", name, "-d", containerDir, "-f")

	harness.WaitForAddonReady(t, m, name, tc.addon, 60*time.Second)
	harness.WaitForEnvVar(t, m, name, "SQLITE_PATH", 2*time.Minute)
	harness.WaitForAppReady(t, m, name, 3*time.Minute)

	return name
}

func postNote(t *testing.T, m *harness.Miren, host, body string) {
	t.Helper()

	r := m.RunCmd("curl", "-sk", "-X", "POST", "-d", body,
		"-H", fmt.Sprintf("Host: %s", host),
		"-w", "\n%{http_code}",
		"https://localhost:443/notes")
	r.RequireSuccess(t)
	r.RequireContains(t, "201")
}

func requireNotes(t *testing.T, m *harness.Miren, host, want string) {
	t.Helper()

	code, body, err := harness.HTTPGet(m, host, "/notes")
	if err != nil {
		t.Fatalf("failed to read notes: %v", err)
	}
	if code != 200 {
		t.Fatalf("expected 200 reading notes, got %d (body: %q)", code, body)
	}
	if strings.TrimSpace(body) != want {
		t.Fatalf("notes mismatch: want %q, got %q", want, strings.TrimSpace(body))
	}
}

// ltxFileListing returns the LTX files the coordinator has stored. It reads the
// coordinator's filesystem as root, which only the dev topology supports.
func ltxFileListing(t *testing.T, m *harness.Miren) string {
	t.Helper()

	r := m.RunCmdAsRoot("sh", "-c", "find /var/lib/miren/sqlite-backups -name '*.ltx' 2>/dev/null | sort")
	return strings.TrimSpace(r.Stdout)
}
