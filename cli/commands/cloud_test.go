package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/mflags"
)

func runCloudCommand(t *testing.T, name string, args ...string) (*CommandOutput, error) {
	t.Helper()
	d := mflags.NewDispatcher("miren")
	registerCloudCommands(d)
	cmd, ok := d.GetCommand(name).(*Cmd)
	require.True(t, ok, "command %s is registered", name)
	return RunCommand(cmd.f.Interface(), args...)
}

func TestCloudCommands(t *testing.T) {
	appArgs := []string{"--organization", "org-acme", "--cluster", "cluster-prod", "--app", "web/blue ?#%"}
	appPath := "/api/v1/organizations/org-acme/apps/cluster-prod/web%2Fblue%20%3F%23%25"
	cases := []struct {
		name, path, response string
		args                 []string
	}{
		{"cloud org list", "/api/v1/organizations", `{"organizations":[{"id":"org-acme","name":"Acme","role":"member"}]}`, nil},
		{"cloud cluster list", "/api/v1/users/clusters", `{"clusters":[{"xid":"cluster-prod","name":"prod","organization_name":"Acme"}]}`, nil},
		{"cloud app list", "/api/v1/organizations/org-acme/apps", `{"apps":[{"name":"web","sample":null}]}`, []string{"--organization", "org-acme"}},
		{"cloud cluster apps", "/api/v1/organizations/org-acme/clusters/cluster-prod/apps", `{"apps":[]}`, []string{"--organization", "org-acme", "--cluster", "cluster-prod"}},
		{"cloud app show", appPath, `{"name":"web","sample":null,"incidents":[]}`, appArgs},
		{"cloud app deployments", appPath + "/deployments", `{"deployments":[],"truncated":true}`, appArgs},
		{"cloud app deployment", appPath + "/deployments/deploy-1", `{"id":"deploy-1","git":{"sha":null}}`, append(append([]string{}, appArgs...), "--deployment", "deploy-1")},
		{"cloud app routes", appPath + "/routes", `{"routes":[],"synced":false,"report_state":"syncing","last_commit_at":null}`, appArgs},
		{"cloud app logs", appPath + "/logs", `{"entries":[{"line":"hello"}],"truncated":true,"count":1,"scanned":9007199254740993}`, append(append([]string{}, appArgs...), "--kind", "build", "--version", "v3", "--service", "worker & web", "--since", "30m", "--limit", "7")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, tc.path, r.URL.EscapedPath())
				require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				if tc.name == "cloud app logs" {
					require.Equal(t, url.Values{"kind": {"build"}, "version": {"v3"}, "service": {"worker & web"}, "since": {"30m"}, "limit": {"7"}}, r.URL.Query())
				} else {
					require.Empty(t, r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			configWithIdentity(t, srv.URL)
			out, err := runCloudCommand(t, tc.name, append(append([]string{}, tc.args...), "--format", "json")...)
			require.NoError(t, err)
			require.JSONEq(t, tc.response, out.Stdout.String())
			require.Contains(t, out.Stderr.String(), "Using identity")
			if tc.name == "cloud app logs" {
				require.Contains(t, out.Stdout.String(), "9007199254740993", "integer precision survives the conversion")
			}
		})
	}
}

func TestCloudApproval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/clusters/register/reg-123/approve", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, map[string]any{"organization_id": "org-acme", "registration_token": "signed-token", "cluster_name": "renamed"}, body)
		_, _ = w.Write([]byte(`{"status":"approved","cluster_id":"cluster-new","cluster_name":"renamed","organization_id":"org-acme"}`))
	}))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	out, err := runCloudCommand(t, "cloud cluster approve", "--registration", "reg-123", "--registration-token", "signed-token", "--organization", "org-acme", "--name", "renamed")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "approved")
	require.Contains(t, out.Stdout.String(), "cluster-new")
	require.NotContains(t, out.Stdout.String(), "signed-token")
}

func TestCloudValidationAndErrors(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "organization access denied", http.StatusForbidden)
	}))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"cloud app show", nil, "--organization is required"},
		{"cloud app show", []string{"--organization", "org"}, "--cluster is required"},
		{"cloud app show", []string{"--organization", "org", "--cluster", "cluster"}, "--app is required"},
		{"cloud cluster approve", []string{"--registration", "reg"}, "--registration-token is required"},
		{"cloud cluster approve", []string{"--registration", "reg", "--registration-token", "token"}, "--organization is required"},
		{"cloud app list", []string{"--organization", ".."}, "dot path segment"},
		{"cloud org list", []string{"--format", "yaml"}, "unsupported cloud output format"},
	} {
		_, err := runCloudCommand(t, tc.name, tc.args...)
		require.ErrorContains(t, err, tc.want)
	}
	require.Zero(t, requests, "invalid inputs never send a request")
	out, err := runCloudCommand(t, "cloud org list", "--json")
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "organization access denied")
	require.Empty(t, out.Stdout.String())
}

func TestCloudTextOutput(t *testing.T) {
	response := `{"apps":[{"name":"web","cluster_name":"prod","sample":{"health":"degraded","ready_instances":2,"desired_instances":5,"observed_at":"2026-10-12T10:00:00Z"}},{"name":"worker","cluster_name":"staging","sample":null}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	out, err := runCloudCommand(t, "cloud app list", "--organization", "org")
	require.NoError(t, err)
	require.Regexp(t, `web\s+prod\s+degraded\s+2\s+5\s+2026-10-12T10:00:00Z`, out.Stdout.String())
	require.Regexp(t, `worker\s+staging\s+-\s+-\s+-\s+-`, out.Stdout.String(), "unknown samples must not look like zero instances")
	response = `{"organizations":null}`
	out, err = runCloudCommand(t, "cloud org list")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "No results")
	response = `{"entries":[{"line":"build failed"}],"truncated":true}`
	out, err = runCloudCommand(t, "cloud app logs", "--organization", "org", "--cluster", "cluster", "--app", "web")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "build failed")
	require.Contains(t, out.Stdout.String(), "Results truncated")
}
