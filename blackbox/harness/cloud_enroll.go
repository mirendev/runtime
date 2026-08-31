package harness

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// NewCloudControlPlane boots just the cloud control plane — build, migrate,
// start — without a POP, without pre-registering a cluster, and without writing
// a registration.json. It is for tests that drive the runtime's own
// registration commands against a real cloud, where the whole point is that the
// runtime does the registering.
//
// Cloud is torn down with the rest of the background processes via t.Cleanup.
func NewCloudControlPlane(t *testing.T, m *Miren) *CloudEnv {
	t.Helper()

	cloudRepo := detectCloudRepo(t, m)

	env := &CloudEnv{
		cloudPort: defaultCloudPort,
		CloudURL:  fmt.Sprintf("http://localhost:%s", defaultCloudPort),
		t:         t,
		m:         m,
	}

	env.cleanupStaleProcesses(t)
	env.buildBinaries(t, cloudRepo)
	env.applyMigrations(t, cloudRepo)
	env.startCloud(t)

	return env
}

// MintEnrollToken creates an organization owned by a fresh dev user and mints
// an enroll token scoped to it. It returns the token (prefix met_) and the
// organization's XID.
//
// Both steps go through the same real user-facing APIs an operator would:
// creating an organization makes the caller its admin, and minting is an
// admin-only action on that organization. No admin RPC is involved, so this
// does not depend on privileged methods a given cloud build may or may not
// expose.
//
// It skips — rather than fails — when the cloud build under test predates
// unattended enrollment (the mint route is absent), matching how the other
// cloud tests stay honest against an older cloud on the default branch.
func (env *CloudEnv) MintEnrollToken(t *testing.T, defaultTags map[string]string) (token, orgID string) {
	t.Helper()

	userToken, _ := env.devLogin(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	created := env.cloudAPI(t, userToken, "POST", "/api/v1/organizations",
		map[string]string{"name": "bb-enroll-org-" + suffix})
	orgID = getString(nested(created, "organization"), "id")
	if orgID == "" {
		t.Fatalf("organization create returned no id: %v", created)
	}

	body, err := json.Marshal(map[string]any{"default_tags": defaultTags})
	if err != nil {
		t.Fatalf("failed to marshal mint request: %v", err)
	}

	// Capture the HTTP status so a cloud without the mint route (older build)
	// skips instead of failing.
	r := env.m.RunCmd("curl", "-s", "-w", "\n%{http_code}", "-X", "POST",
		env.CloudURL+"/api/v1/organizations/"+orgID+"/enroll-tokens",
		"-H", "Authorization: Bearer "+userToken,
		"-H", "Content-Type: application/json",
		"-d", string(body))
	if !r.Success() {
		t.Fatalf("mint request failed to run (exit %d): %s", r.ExitCode, r.Stderr)
	}

	out := strings.TrimSpace(r.Stdout)
	nl := strings.LastIndex(out, "\n")
	if nl < 0 {
		t.Fatalf("mint response had no status line: %q", out)
	}
	payload, status := out[:nl], strings.TrimSpace(out[nl+1:])

	if status == "404" {
		t.Skipf("cloud build has no enroll-token mint route yet; unattended enrollment not available")
	}
	if status != "201" {
		t.Fatalf("mint returned status %s: %s", status, payload)
	}

	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("failed to parse mint response: %v\nraw: %s", err, payload)
	}
	if resp.Token == "" {
		t.Fatalf("mint returned no token: %s", payload)
	}

	t.Logf("minted enroll token for org %s", orgID)
	return resp.Token, orgID
}
