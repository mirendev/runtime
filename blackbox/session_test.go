//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func sessionField(doc entityDoc, name string) any {
	for _, facet := range doc.Facets {
		if facet.Label != "session/session" && facet.Label != "compute/sandbox" && facet.Label != "session/binding" && facet.Label != "session/slot" {
			continue
		}
		for _, field := range facet.Fields {
			if field.Name == name {
				return field.Value
			}
			if facet.Label == "compute/sandbox" && field.Name == "session_info" {
				nested := map[string]string{"session": "owner", "session_group": "group"}[name]
				if nested != "" {
					if value, ok := findNestedString(field.Value, nested); ok {
						return value
					}
				}
			}
		}
	}
	return nil
}

func TestSessionDedicatedAndSharedSandboxes(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	app := harness.DeployApp(t, m, harness.AppOptions{Testdata: "go-server"})

	file, err := os.CreateTemp(c.RepoRoot, ".blackbox-session-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	file.Close()
	t.Cleanup(func() { _ = os.Remove(path) })
	create := func(name string, capacity int, group ...string) string {
		t.Helper()
		args := []string{"session", "create", "-a", app, "--name", name}
		if len(group) > 0 {
			args = append(args, "--group", group[0])
		}
		if capacity > 1 {
			args = append(args, "--max-sessions-per-sandbox", fmt.Sprint(capacity))
		}
		id := strings.TrimSpace(m.MustRun(args...).Stdout)
		if id != "session/"+app+"-"+name {
			t.Fatalf("unexpected created Session ID %q", id)
		}
		t.Cleanup(func() {
			if r := m.Run("debug", "entity", "delete", "-i", id); !r.Success() {
				t.Errorf("deleting session %s: %s", id, r.Stderr)
				return
			}
			if capacity > 1 {
				bindingID := "session_binding/" + strings.ReplaceAll(id, "/", "__")
				var deleted string
				harness.Poll(t, id+" deletion recorded", time.Minute, 3*time.Second, func() (bool, string) {
					for _, doc := range listDocs(t, m, "binding") {
						if doc.Id == bindingID {
							deleted, _ = sessionField(doc, "deleted_at").(string)
							return deleted != "", fmt.Sprintf("deleted_at=%q", deleted)
						}
					}
					return false, "binding not found"
				})
				// The fixture does not implement Session cleanup. Explicitly release
				// its reservation so the test doesn't leave running hosts behind.
				ack := fmt.Sprintf("id: %s\nkind: dev.miren.session/binding\nversion: v1alpha\nspec:\n  acknowledged_at: %q\n", bindingID, deleted)
				if err := os.WriteFile(path, []byte(ack), 0o600); err != nil {
					t.Error(err)
					return
				}
				if r := m.Run("debug", "entity", "patch", "-i", bindingID, "-p", m.ContainerPath(path)); !r.Success() {
					t.Errorf("acknowledging %s: %s", id, r.Stderr)
				}
			}
		})
		return id
	}
	get := func(id string) entityDoc {
		t.Helper()
		r := m.MustRun("debug", "entity", "get", "-i", id, "--json")
		var doc entityDoc
		if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil {
			t.Fatalf("decoding %s: %v", id, err)
		}
		return doc
	}
	waitReady := func(id string) string {
		t.Helper()
		var sandbox string
		harness.Poll(t, id+" ready", 2*time.Minute, 3*time.Second, func() (bool, string) {
			doc := get(id)
			phase, _ := sessionField(doc, "phase").(string)
			sandbox, _ = sessionField(doc, "sandbox").(string)
			return phase == "ready" && sandbox != "", fmt.Sprintf("phase=%s sandbox=%s", phase, sandbox)
		})
		return sandbox
	}

	dedicatedA := create("dedicated-a", 1)
	dedicatedB := create("dedicated-b", 0)
	dedicatedHostA := waitReady(dedicatedA)
	dedicatedHostB := waitReady(dedicatedB)
	if dedicatedHostA == dedicatedHostB {
		t.Fatalf("dedicated Sessions share sandbox %s", dedicatedHostA)
	}
	for id, host := range map[string]string{dedicatedA: dedicatedHostA, dedicatedB: dedicatedHostB} {
		if owner := sessionField(get(host), "session"); owner != id {
			t.Fatalf("dedicated sandbox %s owner = %v, want %s", host, owner, id)
		}
	}

	sharedA := create("shared-a", 2)
	sharedB := create("shared-b", 2)
	sharedHost := waitReady(sharedA)
	if other := waitReady(sharedB); other != sharedHost {
		t.Fatalf("shared Sessions got different sandboxes: %s, %s", sharedHost, other)
	}
	if owner := sessionField(get(sharedHost), "session"); owner != nil {
		t.Fatalf("shared sandbox %s has dedicated owner %v", sharedHost, owner)
	}
	if group := sessionField(get(sharedHost), "session_group"); group == nil || group == "" {
		t.Fatalf("shared sandbox %s has no session group", sharedHost)
	}
	info := m.MustRun("session", "get", sharedB, "--format", "json")
	var reported struct {
		MaxSessions int64  `json:"max_sessions_per_sandbox"`
		Sandbox     string `json:"sandbox"`
	}
	if err := json.Unmarshal([]byte(info.Stdout), &reported); err != nil {
		t.Fatal(err)
	}
	if reported.MaxSessions != 2 || reported.Sandbox != sharedHost {
		t.Fatalf("session get returned wrong shared assignment: %+v", reported)
	}
	listing := m.MustRun("session", "list", "--format", "json")
	var sessions []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(listing.Stdout), &sessions); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sessions {
		found = found || s.ID == sharedB
	}
	if !found {
		t.Fatalf("session list omitted %s", sharedB)
	}
	sharedC := create("shared-c", 2)
	if other := waitReady(sharedC); other == sharedHost || other == dedicatedHostA || other == dedicatedHostB {
		t.Fatalf("capacity-two shared sandbox did not allocate a new host: %s", other)
	}
	groupedA := create("grouped-a", 2, "queue")
	groupedHost := waitReady(groupedA)
	if groupedHost == sharedHost || groupedHost == waitReady(sharedC) {
		t.Fatalf("grouped Session joined an ungrouped sandbox: %s", groupedHost)
	}
	groupedB := create("grouped-b", 2, "queue")
	if other := waitReady(groupedB); other != groupedHost {
		t.Fatalf("matching group keys did not share a sandbox: %s, %s", groupedHost, other)
	}

	m.MustRun("session", "delete", sharedA)
	harness.Poll(t, "shared deletion notification", time.Minute, 3*time.Second, func() (bool, string) {
		for _, doc := range listDocs(t, m, "binding") {
			if sessionField(doc, "session") == sharedA {
				deleted := sessionField(doc, "deleted_at")
				return deleted != nil && deleted != "", fmt.Sprintf("deleted_at=%v", deleted)
			}
		}
		return false, "binding not found"
	})
	if host := waitReady(sharedB); host != sharedHost {
		t.Fatalf("deleting another Session moved live shared Session to %s", host)
	}
	var reservations int
	for _, doc := range listDocs(t, m, "slot") {
		if sessionField(doc, "sandbox") == sharedHost {
			reservations++
		}
	}
	if reservations != 2 {
		t.Fatalf("deleted Session released capacity before workload acknowledgment: %d reservations, want 2", reservations)
	}

	// A dedicated Session, unlike a shared Session, shuts down its sandbox on
	// suspension and gets a new incarnation when resumed.
	m.MustRun("session", "suspend", dedicatedA)
	harness.Poll(t, "dedicated Session inactive", 2*time.Minute, 3*time.Second, func() (bool, string) {
		phase := sessionField(get(dedicatedA), "phase")
		return phase == "inactive", fmt.Sprintf("phase=%v", phase)
	})
	m.MustRun("session", "resume", dedicatedA)
	if next := waitReady(dedicatedA); next == dedicatedHostA {
		t.Fatalf("dedicated Session resumed on old sandbox %s", next)
	}

	oldVersion := sessionField(get(sharedB), "version")
	oldDedicated := waitReady(dedicatedA)
	m.MustRun("deploy", "-a", app, "-d", m.ContainerPath(filepath.Join(c.RepoRoot, "testdata/go-server")),
		"-f", "-e", "SESSION_DEPLOY=next")
	for id, previous := range map[string]string{dedicatedA: oldDedicated, sharedB: sharedHost} {
		harness.Poll(t, id+" follows app deploy", 4*time.Minute, 3*time.Second, func() (bool, string) {
			doc := get(id)
			version := sessionField(doc, "version")
			host := sessionField(doc, "sandbox")
			phase := sessionField(doc, "phase")
			return version != oldVersion && version != nil && host != previous && phase == "ready",
				fmt.Sprintf("version=%v sandbox=%v phase=%v", version, host, phase)
		})
	}
}
