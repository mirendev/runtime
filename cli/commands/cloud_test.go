package commands

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	appArgs := []string{"--organization", "org-acme", "-C", "cluster-prod", "-a", "web/blue ?#%"}
	appPath := "/api/v1/organizations/org-acme/apps/cluster-prod/web%2Fblue%20%3F%23%25"
	cases := []struct {
		name, path, response string
		args                 []string
	}{
		{"org list", "/api/v1/organizations", `{"organizations":[{"id":"org-acme","name":"Acme","role":"member"}]}`, nil},
		{"cloud cluster list", "/api/v1/users/clusters", `{"clusters":[{"xid":"cluster-prod","name":"prod","organization_name":"Acme"}]}`, nil},
		{"cloud app list", "/api/v1/organizations/org-acme/apps", `{"apps":[{"name":"web","sample":null}]}`, []string{"--organization", "org-acme"}},
		{"cloud cluster apps", "/api/v1/organizations/org-acme/clusters/cluster-prod/apps", `{"apps":[]}`, []string{"--organization", "org-acme", "cluster-prod"}},
		{"cloud app show", appPath, `{"name":"web","sample":null,"incidents":[]}`, appArgs},
		{"cloud app history", appPath + "/deployments", `{"deployments":[],"truncated":true}`, appArgs},
		{"cloud deployment show", appPath + "/deployments/deploy-1", `{"id":"deploy-1","git":{"sha":null}}`, append(append([]string{}, appArgs...), "deploy-1")},
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
	out, err := runCloudCommand(t, "cloud cluster approve", "reg-123", "--registration-token", "signed-token", "--organization", "org-acme", "--name", "renamed")
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
		{"cloud app show", nil, "--cluster is required"},
		{"cloud app show", []string{"--organization", "org"}, "--cluster is required"},
		{"cloud app show", []string{"--organization", "org", "--cluster", "cluster"}, "--app is required"},
		{"cloud cluster approve", []string{"reg"}, "--registration-token is required"},
		{"cloud app show", []string{"--organization", "org-a", "--cluster", "..", "--app", "web"}, "dot path segment"},
		{"org list", []string{"--format", "yaml"}, "unsupported cloud output format"},
	} {
		_, err := runCloudCommand(t, tc.name, tc.args...)
		require.ErrorContains(t, err, tc.want)
	}
	require.Zero(t, requests, "invalid inputs never send a request")
	out, err := runCloudCommand(t, "org list", "--json")
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
	out, err := runCloudCommand(t, "cloud app list", "--organization", "org-acme")
	require.NoError(t, err)
	require.Regexp(t, `web\s+prod\s+degraded\s+2\s+5\s+2026-10-12T10:00:00Z`, out.Stdout.String())
	require.Regexp(t, `worker\s+staging\s+-\s+-\s+-\s+-`, out.Stdout.String(), "unknown samples must not look like zero instances")
	response = `{"organizations":null}`
	out, err = runCloudCommand(t, "org list")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "No results")
	response = `{"entries":[{"line":"build failed"}],"truncated":true}`
	out, err = runCloudCommand(t, "cloud app logs", "--organization", "org-acme", "--cluster", "cluster", "--app", "web")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "build failed")
	require.Contains(t, out.Stdout.String(), "Results truncated")
	response = `{"keys":[{"id":17,"xid":"key-a","name":null,"key_type":"ed25519","status":"revoked","is_primary":false,"last_used_at":null}]}`
	out, err = runCloudCommand(t, "account key list")
	require.NoError(t, err)
	require.Regexp(t, `key-a\s+-\s+ed25519\s+revoked\s+false\s+-`, out.Stdout.String())
	response = `{"statuses":[{"cluster_xid":"clu-a","online":true},{"cluster_xid":"clu-b","online":false}]}`
	out, err = runCloudCommand(t, "cloud cluster online-batch", "--clusters", `["clu-a","clu-b","inaccessible"]`)
	require.NoError(t, err)
	require.Regexp(t, `clu-a\s+true`, out.Stdout.String())
	require.Regexp(t, `clu-b\s+false`, out.Stdout.String())
	require.NotContains(t, out.Stdout.String(), "inaccessible")
}

func TestCloudInitOutput(t *testing.T) {
	snippet := "#include-once\nhttps://cloud.example.test/boot/bootstrap-test\n"
	response := map[string]any{
		"id": "enr-test", "token": "secret-test", "expires_at": "2026-10-17T12:00:00Z",
		"user_data": snippet, "cloud_config": "#cloud-config\npackages:\n  - curl\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/organizations/org-acme/enroll-tokens", r.URL.Path)
		require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, map[string]any{"ttl_hours": float64(12), "default_tags": map[string]any{"region": "west"}}, body)
		w.WriteHeader(http.StatusCreated)
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	args := []string{"-o", "org-acme", "--ttl-hours", "12", "--default-tags", `{"region":"west"}`}
	out, err := runCloudCommand(t, "cloud cluster cloud-init", args...)
	require.NoError(t, err)
	require.Equal(t, snippet, out.Stdout.String(), "redirected output must be usable user-data without labels or JSON quoting")
	require.Contains(t, out.Stderr.String(), "Using identity")
	out, err = runCloudCommand(t, "cloud cluster cloud-init", append(args, "--json")...)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(out.Stdout.Bytes(), &document))
	require.Equal(t, response, document)
	for _, invalid := range []any{nil, "", true} {
		response["user_data"] = invalid
		out, err = runCloudCommand(t, "cloud cluster cloud-init", args...)
		require.ErrorContains(t, err, `"user_data" is not a nonempty string`)
		require.Empty(t, out.Stdout.String())
	}
}

func TestCloudResourceCommands(t *testing.T) {
	org := "-o org-a"
	cluster := org + " clu-b"
	group := "grp-c"
	cases := []struct {
		name, method, uri, flags, body, response string
		jsonArgs                                 []string
	}{
		{"account show", "GET", "/api/v1/me", "", "", `{"user":{"name":"Ada"},"organizations":[]}`, nil},
		{"account update", "PUT", "/api/v1/me", "--name Ada --email ada@example.test --clear-avatar", `{"name":"Ada","email":"ada@example.test","avatar_url":""}`, `{"status":"success","user":{"name":"Ada"}}`, nil},
		{"account update", "PUT", "/api/v1/me", "--name Ada --email ada@example.test --avatar-url https://example.test/avatar", `{"name":"Ada","email":"ada@example.test","avatar_url":"https://example.test/avatar"}`, `{"status":"success"}`, nil},
		{"account identity list", "GET", "/api/v1/identities", "", "", `{"identities":[{"id":"123","provider":"github","username":"ada"}]}`, nil},
		{"account identity unlink", "DELETE", "/api/v1/identities/google/sub%2Fone%3F", "--provider google sub/one? --yes", "", "null", nil},
		{"account key list", "GET", "/api/v1/users/keys", "", "", `{"keys":[{"id":17,"xid":"key-a","name":null,"last_used_at":null,"is_primary":false,"status":"revoked"}]}`, nil},
		{"account key revoke", "DELETE", "/api/v1/users/keys/key-a", "key-a -y", "", "null", nil},
		{"account token revoke-all", "POST", "/auth/revoke/user", "--yes", `{"reason":""}`, `{"status":"success","message":"All user tokens revoked successfully"}`, nil},
		{"account token revoke-all", "POST", "/auth/revoke/user", "--reason compromised --yes", `{"reason":"compromised"}`, `{"status":"success"}`, nil},
		{"invite show", "GET", "/api/v1/invitations/link%2Fone%3F", "link/one?", "", `{"organization_id":"org-a","expires_at":null,"already_member":false}`, nil},
		{"invite accept", "POST", "/api/v1/invitations/link-token/accept", "link-token", "", `{"organization_id":"org-a","success":true}`, nil},
		{"invite accept", "POST", "/api/v1/invitations/link-token/accept", "link-token", "", `{"organization_id":"org-a","already_member":true}`, nil},
		{"user search", "GET", "/api/v1/users/search?q=ada%2Bops", "ada+ops", "", `{"users":[{"id":"usr-new","name":"Ada","email":"ada@example.test"}]}`, nil},
		{"cloud cluster delete", "DELETE", "/api/v1/organizations/org-a/clusters/clu-b", cluster + " --yes", "", "null", nil},
		{"cloud cluster online-batch", "POST", "/api/v1/cluster-channel/online", "", `{"clusters":["clu-b","clu-c","inaccessible"]}`, `{"statuses":[{"cluster_xid":"clu-b","online":true},{"cluster_xid":"clu-c","online":false}]}`, []string{"--clusters", `["clu-b","clu-c","inaccessible"]`}},
		{"cloud cluster online-batch", "POST", "/api/v1/cluster-channel/online", "--clusters []", `{"clusters":[]}`, `{"statuses":[]}`, nil},
		{"org show", "GET", "/api/v1/organizations/org-a", "org-a", "", `{"organization":{"id":"org-a"},"members":[]}`, nil},
		{"org member-list", "GET", "/api/v1/organizations/org-a", org, "", `{"members":[{"id":"usr-d","role":"member"}]}`, nil},
		{"org cluster-list", "GET", "/api/v1/organizations/org-a/clusters", org, "", `{"clusters":[{"id":"clu-b","node_count":null,"app_count":0}],"total":1}`, nil},
		{"org create", "POST", "/api/v1/organizations", "new-org", `{"name":"new-org"}`, `{"organization":{"id":"org-new","role":"admin"}}`, nil},
		{"org add-member", "POST", "/api/v1/organizations/org-a/members", org + " usr-d", `{"user_id":"usr-d"}`, `{"user_id":"usr-d","role":"member"}`, nil},
		{"org set-member-role", "PUT", "/api/v1/organizations/org-a/users/usr-d/role", org + " usr-d --role admin", `{"role":"admin"}`, `{"role":"admin"}`, nil},
		{"org remove-member", "DELETE", "/api/v1/organizations/org-a/members/usr-d", org + " usr-d --yes", "", "null", nil},
		{"org member-search", "GET", "/api/v1/organizations/org-a/users/search?q=ada%2Bops", org + " ada+ops", "", `{"status":"success","users":[]}`, nil},
		{"cloud cluster show", "GET", "/api/v1/organizations/org-a/clusters/clu-b", cluster, "", `{"id":"clu-b","resources":null}`, nil},
		{"cloud cluster nodes", "GET", "/api/v1/organizations/org-a/clusters/clu-b/nodes", cluster, "", `{"nodes":[],"synced":false,"last_commit_at":null}`, nil},
		{"cloud cluster routes", "GET", "/api/v1/organizations/org-a/clusters/clu-b/routes", cluster, "", `{"routes":[],"synced":false,"report_state":"syncing"}`, nil},
		{"cloud cluster server", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server", cluster, "", `{"available":false,"connected":false}`, nil},
		{"cloud cluster upgrade-policy", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server/upgrade-policy", cluster, "", `{"enabled":false,"channel":"stable"}`, nil},
		{"cloud cluster online", "GET", "/api/v1/cluster-channel/clusters/clu-b/online", "clu-b", "", `{"cluster_xid":"clu-b","online":false}`, nil},
		{"cloud cluster update", "PATCH", "/api/v1/organizations/org-a/clusters/clu-b", cluster + " --anywhere-mode never --clear-description", `{"description":"","tags":{"region":"west"},"miren_anywhere_mode":"never"}`, `{"message":"Cluster updated successfully"}`, []string{"--tags", `{"region":"west"}`}},
		{"cloud cluster update", "PATCH", "/api/v1/organizations/org-a/clusters/clu-b", cluster, `{"description":"Production \"west\""}`, `{"message":"Cluster updated successfully"}`, []string{"--description", `Production "west"`}},
		{"cloud operation list", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server/operations?limit=7", org + " -C clu-b --limit 7", "", `{"operations":[{"id":"op-e","status":"failed"}]}`, nil},
		{"cloud operation show", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server/operations/op-e", org + " -C clu-b op-e", "", `{"id":"op-e","status":"failed","error":"upgrade failed"}`, nil},
		{"group list", "GET", "/api/v1/organizations/org-a/groups", org, "", `{"groups":[{"id":"grp-c","is_default":false,"member_count":0}]}`, nil},
		{"group show", "GET", "/api/v1/groups/grp-c", group, "", `{"group":{"id":"grp-c"}}`, nil},
		{"group member-list", "GET", "/api/v1/groups/grp-c/members", group, "", `{"members":[{"user_id":"usr-d"}]}`, nil},
		{"group create", "POST", "/api/v1/organizations/org-a/groups", org + " ops --description Operators", `{"name":"ops","description":"Operators"}`, `{"group":{"id":"grp-c","name":"ops"}}`, nil},
		{"group update", "PUT", "/api/v1/groups/grp-c", group + " --name operations --clear-description", `{"name":"operations","description":""}`, `{"group":{"id":"grp-c","name":"operations"}}`, nil},
		{"group add-member", "POST", "/api/v1/groups/grp-c/members", "--group grp-c usr-d", `{"user_xid":"usr-d"}`, `{"member":{"user_id":"usr-d"}}`, nil},
		{"group remove-member", "DELETE", "/api/v1/groups/grp-c/members/usr-d", "--group grp-c usr-d --yes", "", `{"status":"success","message":"removed"}`, nil},
		{"group delete", "DELETE", "/api/v1/groups/grp-c", group + " --yes", "", "null", nil},
		{"invite list", "GET", "/api/v1/organizations/org-a/invite-links", org, "", `{"invite_links":[]}`, nil},
		{"invite create", "POST", "/api/v1/organizations/org-a/invite-link", org + " --expires-in 30d --max-uses 3", `{"expires_in":"30d","max_uses":3,"groups":["grp-c","grp-f"]}`, `{"id":"inv-g","invite_link":"/invite/secret"}`, []string{"--groups", `["grp-c","grp-f"]`}},
		{"invite update", "PATCH", "/api/v1/organizations/org-a/invite-links/inv-g", org + " inv-g --max-uses null --groups []", `{"max_uses":null,"groups":[]}`, "null", nil},
		{"invite update", "PATCH", "/api/v1/organizations/org-a/invite-links/inv-g", org + " inv-g --expires-in 1d", `{"expires_in":"1d"}`, "null", nil},
		{"invite url", "GET", "/api/v1/organizations/org-a/invite-links/inv-g/url", org + " inv-g", "", `{"invite_link":"/invite/secret"}`, nil},
		{"invite revoke", "DELETE", "/api/v1/organizations/org-a/invite-links/inv-g", org + " inv-g --yes", "", "null", nil},
		{"enroll-token create", "POST", "/api/v1/organizations/org-a/enroll-tokens", org + " --ttl-hours 12", `{"ttl_hours":12,"default_tags":{"region":"west"}}`, `{"id":"enr-h","token":"secret","cloud_config":"bootstrap"}`, []string{"--default-tags", `{"region":"west"}`}},
		{"enroll-token create", "POST", "/api/v1/organizations/org-a/enroll-tokens", org, "", `{"id":"enr-h","token":"secret","cloud_config":"bootstrap"}`, nil},
		{"cloud cluster cloud-init", "POST", "/api/v1/organizations/org-a/enroll-tokens", org, "", `{"id":"enr-h","token":"secret","user_data":"#include-once\nhttps://cloud.example.test/boot/test\n","cloud_config":"#cloud-config\n"}`, nil},
		{"enroll-token list", "GET", "/api/v1/organizations/org-a/enroll-tokens?limit=9&offset=13", org + " --limit 9 --offset 13", "", `{"enroll_tokens":[{"id":"enr-h","state":"pending"}]}`, nil},
		{"enroll-token show", "GET", "/api/v1/organizations/org-a/enroll-tokens/enr-h", org + " enr-h", "", `{"id":"enr-h","state":"pending"}`, nil},
		{"enroll-token revoke", "DELETE", "/api/v1/organizations/org-a/enroll-tokens/enr-h", org + " enr-h --yes", "", "null", nil},
		{"subdomain base-domain-list", "GET", "/api/v1/subdomains/base-domains", "", "", `{"domains":[{"domain":"run.garden","enabled":true}]}`, nil},
		{"subdomain list", "GET", "/api/v1/organizations/org-a/subdomains", org, "", `{"subdomains":[{"id":"sub-i","assignment":null}],"total":1}`, nil},
		{"subdomain claim", "POST", "/api/v1/organizations/org-a/subdomains", org + " hello --base-domain run.garden", `{"name":"hello","base_domain":"run.garden"}`, `{"id":"sub-i","full_hostname":"hello.run.garden"}`, nil},
		{"subdomain assign", "POST", "/api/v1/organizations/org-a/subdomains/sub-i/assign", org + " sub-i --cluster clu-b", `{"cluster_id":"clu-b"}`, `{"cluster_id":"clu-b","status":"pending_dns"}`, nil},
		{"subdomain unassign", "POST", "/api/v1/organizations/org-a/subdomains/sub-i/unassign", org + " sub-i --yes", "", `{"message":"Subdomain unassigned successfully"}`, nil},
		{"subdomain delete", "DELETE", "/api/v1/organizations/org-a/subdomains/sub-i", org + " sub-i --yes", "", "null", nil},
		{"volume list", "GET", "/api/v1/organizations/org-a/volumes?cursor=next%2Bpage&limit=11", org + " --cursor next+page --limit 11", "", `{"volumes":[],"next_cursor":"page-3"}`, nil},
		{"volume updates", "GET", "/api/v1/organizations/org-a/volumes/vol-j/updates?cursor=snapshot%3Akey%2Fone&kind=snapshot&limit=17", org + " vol-j --cursor snapshot:key/one --kind snapshot --limit 17", "", `{"updates":[],"next_cursor":"snapshot:key-two"}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tc.method, r.Method)
				require.Equal(t, tc.uri, r.URL.RequestURI())
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if tc.body == "" {
					require.Empty(t, body)
				} else {
					require.JSONEq(t, tc.body, string(body))
				}
				if tc.response == "null" {
					w.WriteHeader(http.StatusNoContent)
				} else {
					_, _ = w.Write([]byte(tc.response))
				}
			}))
			defer srv.Close()
			configWithIdentity(t, srv.URL)
			args := append(strings.Fields(tc.flags), tc.jsonArgs...)
			out, err := runCloudCommand(t, tc.name, append(args, "--json")...)
			require.NoError(t, err)
			require.JSONEq(t, tc.response, out.Stdout.String())
		})
	}
}

func TestCloudResourceValidation(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"invite update", []string{"--organization", "org", "inv", "--groups", `[`}, "valid JSON"},
		{"invite update", []string{"--organization", "org", "inv", "--max-uses", `3 null`}, "exactly one JSON value"},
		{"org set-member-role", []string{"--organization", "org", "user", "--role", "owner"}, "admin"},
		{"org add-member", []string{"--organization", "org"}, "user argument is required"},
		{"subdomain assign", []string{"--organization", "org", "sub"}, "--cluster is required"},
		{"org remove-member", []string{"--organization", "org", "user"}, "--yes"},
		{"group remove-member", []string{"--group", "group", "user"}, "--yes"},
		{"group delete", []string{"group"}, "--yes"},
		{"invite revoke", []string{"--organization", "org", "invite"}, "--yes"},
		{"enroll-token revoke", []string{"--organization", "org", "token"}, "--yes"},
		{"subdomain unassign", []string{"--organization", "org", "sub"}, "--yes"},
		{"subdomain delete", []string{"--organization", "org", "sub"}, "--yes"},
		{"account update", nil, "at least one field"},
		{"group update", []string{"group"}, "at least one field"},
		{"account update", []string{"--avatar-url", "https://example.test/avatar", "--clear-avatar"}, "conflicts"},
		{"group update", []string{"group", "--description", "text", "--clear-description"}, "conflicts"},
		{"cloud cluster update", []string{"--organization", "org", "cluster", "--description", "text", "--clear-description"}, "conflicts"},
		{"user search", nil, "query argument is required"},
		{"account identity unlink", []string{"--provider", "github", "123"}, "--yes"},
		{"account key revoke", []string{"key"}, "--yes"},
		{"account token revoke-all", nil, "--yes"},
		{"invite accept", nil, "token argument is required"},
		{"cloud cluster delete", []string{"--organization", "org", "cluster"}, "--yes"},
		{"cloud cluster online-batch", nil, "--clusters is required"},
	} {
		_, err := runCloudCommand(t, tc.name, tc.args...)
		require.ErrorContains(t, err, tc.want)
	}
	require.Zero(t, requests)
}

func TestCloudPartialUpdates(t *testing.T) {
	for _, tc := range []struct {
		name, path, envelope, initial, want string
		args                                []string
		failRead                            bool
	}{
		{"account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test","avatar_url":"https://example.test/old"}`, `{"name":"New","email":"old@example.test","avatar_url":"https://example.test/old"}`, []string{"--name", "New"}, false},
		{"account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test","avatar_url":"https://example.test/old"}`, `{"name":"Old","email":"old@example.test","avatar_url":""}`, []string{"--clear-avatar"}, false},
		{"account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test"}`, `{"name":"Old","email":"new@example.test","avatar_url":""}`, []string{"--email", "new@example.test"}, false},
		{"account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test","avatar_url":null}`, `{"name":"New","email":"old@example.test","avatar_url":""}`, []string{"--name", "New"}, false},
		{"group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Oncall","description":"Production access"}`, []string{"grp-c", "--name", "Oncall"}, false},
		{"group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Operators","description":"Staging access"}`, []string{"grp-c", "--description", "Staging access"}, false},
		{"group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Operators","description":""}`, []string{"grp-c", "--clear-description"}, false},
		{"group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators"}`, `{"name":"Oncall","description":""}`, []string{"grp-c", "--name", "Oncall"}, false},
		{"group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Operators","description":"Production access"}`, []string{"grp-c", "--name", "Oncall"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stored map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.initial), &stored))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tc.path, r.URL.Path)
				if r.Method == http.MethodGet {
					if tc.failRead {
						http.Error(w, "access denied", http.StatusForbidden)
						return
					}
				} else {
					require.Equal(t, http.MethodPut, r.Method)
					stored = nil
					require.NoError(t, json.NewDecoder(r.Body).Decode(&stored))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{tc.envelope: stored})
			}))
			defer srv.Close()
			configWithIdentity(t, srv.URL)
			out, err := runCloudCommand(t, tc.name, append(tc.args, "--json")...)
			if tc.failRead {
				require.ErrorContains(t, err, "403")
				require.Empty(t, out.Stdout.String())
			} else {
				require.NoError(t, err)
				require.JSONEq(t, `{"`+tc.envelope+`":`+tc.want+`}`, out.Stdout.String())
			}
			data, err := json.Marshal(stored)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(data))
		})
	}
}

func TestCloudPartialUpdatesRejectUnexpectedEnvelope(t *testing.T) {
	for _, command := range []struct {
		name, envelope string
		args           []string
	}{
		{"account update", "user", []string{"--name", "New"}},
		{"group update", "group", []string{"grp-c", "--name", "New"}},
	} {
		for _, response := range []string{"null", "[]", "{}", `{"` + command.envelope + `":null}`, `{"` + command.envelope + `":[]}`} {
			t.Run(command.name+"/"+response, func(t *testing.T) {
				writes := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						writes++
					}
					_, _ = w.Write([]byte(response))
				}))
				defer srv.Close()
				configWithIdentity(t, srv.URL)
				out, err := runCloudCommand(t, command.name, append(command.args, "--json")...)
				require.ErrorContains(t, err, "unexpected response shape")
				require.Empty(t, out.Stdout.String())
				require.Zero(t, writes, "an invalid envelope must not trigger a replacement write")
			})
		}
	}
}

func TestCloudVolumeCursorText(t *testing.T) {
	response := `{"volumes":[{"volume_id":"vol-j","name":"data","declared_size":9007199254740993,"lease":null}],"next_cursor":"page-2"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	out, err := runCloudCommand(t, "volume list", "--organization", "org-acme")
	require.NoError(t, err)
	require.Regexp(t, `vol-j\s+data\s+9007199254740993\s+-`, out.Stdout.String())
	require.Contains(t, out.Stdout.String(), "Next cursor: page-2")
	response = `{"volumes":[],"next_cursor":"page-3"}`
	out, err = runCloudCommand(t, "volume list", "--organization", "org-acme")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "No results")
	require.Contains(t, out.Stdout.String(), "Next cursor: page-3")
}

func TestCloudOrganizationResolution(t *testing.T) {
	one := `{"organizations":[{"id":"org-selected","name":"Acme / West"}]}`
	many := `{"organizations":[{"id":"org-other","name":"Other"},{"id":"org-selected","name":"Acme / West"}]}`
	for _, tc := range []struct {
		name, command, organizations, path, body, wantError string
		args                                                []string
		lookupStatus                                        int
	}{
		{"name in path", "cloud app show", many, "/api/v1/organizations/org-selected/apps/Acme%20%2F%20West/web", "", "", []string{"--organization", "Acme / West", "--cluster", "Acme / West", "--app", "web"}, 200},
		{"dot name is resolved", "org show", `{"organizations":[{"id":"org-selected","name":".."}]}`, "/api/v1/organizations/org-selected", "", "", []string{".."}, 200},
		{"default in path", "org show", one, "/api/v1/organizations/org-selected", "", "", nil, 200},
		{"ID skips lookup", "org show", "", "/api/v1/organizations/org-selected", "", "", []string{"org-selected"}, 0},
		{"short name in body", "cloud cluster approve", many, "/api/v1/clusters/register/reg-a/approve", `{"registration_token":"signed","organization_id":"org-selected"}`, "", []string{"reg-a", "--registration-token", "signed", "-o", "Acme / West"}, 200},
		{"default in body", "cloud cluster approve", one, "/api/v1/clusters/register/reg-a/approve", `{"registration_token":"signed","organization_id":"org-selected"}`, "", []string{"reg-a", "--registration-token", "signed"}, 200},
		{"destructive default", "cloud cluster delete", one, "/api/v1/organizations/org-selected/clusters/clu-a", "", "", []string{"clu-a", "--yes"}, 200},
		{"missing name", "org show", many, "", "", "not found", []string{"Missing"}, 200},
		{"exact name", "org show", one, "", "", "not found", []string{"acme / west"}, 200},
		{"duplicate name", "org show", `{"organizations":[{"id":"org-a","name":"Acme"},{"id":"org-b","name":"Acme"}]}`, "", "", "ambiguous", []string{"Acme"}, 200},
		{"no default", "org show", `{"organizations":[]}`, "", "", "no Cloud organizations", nil, 200},
		{"multiple defaults", "org show", many, "", "", "specify --organization (-o)", nil, 200},
		{"lookup error", "org show", `{"error":"access denied"}`, "", "", "403", []string{"Acme"}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.EscapedPath())
				require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
				if r.URL.Path == "/api/v1/organizations" {
					require.Equal(t, http.MethodGet, r.Method)
					w.WriteHeader(tc.lookupStatus)
					_, _ = w.Write([]byte(tc.organizations))
					return
				}
				require.Equal(t, tc.path, r.URL.EscapedPath())
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if tc.body == "" {
					require.Empty(t, body)
				} else {
					require.JSONEq(t, tc.body, string(body))
				}
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			defer srv.Close()
			configWithIdentity(t, srv.URL)
			out, err := runCloudCommand(t, tc.command, append(tc.args, "--json")...)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Empty(t, out.Stdout.String())
				require.Equal(t, []string{"/api/v1/organizations"}, paths)
			} else {
				require.NoError(t, err)
				require.JSONEq(t, `{"status":"ok"}`, out.Stdout.String())
				wantPaths := []string{tc.path}
				if tc.lookupStatus != 0 {
					wantPaths = append([]string{"/api/v1/organizations"}, wantPaths...)
				}
				require.Equal(t, wantPaths, paths)
			}
		})
	}
}

func TestCloudAccountTextOutput(t *testing.T) {
	response := `{"user":{"id":"usr-demo","name":"Demo User","email":"demo@example.test"},"organizations":[{"id":"org-a","name":"Acme","role":"admin","feature_flags":{"usersubdomains":true,"clouddisks":false,"addcluster":true}},{"id":"org-b","name":"Other","role":"member","feature_flags":{}}],"identities":[{"id":"provider-123","provider":"github","username":"demo","email":"demo@example.test","connected_at":"2026-01-01T00:00:00Z"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/me", r.URL.Path)
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	out, err := runCloudCommand(t, "account show")
	require.NoError(t, err)
	require.Regexp(t, `Name:\s+Demo User`, out.Stdout.String())
	require.Regexp(t, `Email:\s+demo@example\.test`, out.Stdout.String())
	require.Regexp(t, `org-a\s+Acme\s+admin\s+addcluster, usersubdomains`, out.Stdout.String())
	require.Regexp(t, `org-b\s+Other\s+member\s+none`, out.Stdout.String())
	require.Contains(t, out.Stdout.String(), "Linked identities")
	require.Regexp(t, `github\s+demo\s+demo@example.test\s+provider-123`, out.Stdout.String())
	require.NotContains(t, out.Stdout.String(), "clouddisks")
	for _, args := range [][]string{{"--json"}, {"--format", "json"}} {
		out, err = runCloudCommand(t, "account show", args...)
		require.NoError(t, err)
		require.JSONEq(t, response, out.Stdout.String())
	}
	response = `{"user":{"id":"usr-demo","name":"Demo User","email":"demo@example.test"}}`
	out, err = runCloudCommand(t, "account show")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "Organizations\nNot reported")
	require.Contains(t, out.Stdout.String(), "Linked identities\nNot reported")
}

func TestCloudDetailTextOutput(t *testing.T) {
	for _, tc := range []struct {
		command, path, response string
		args                    []string
		patterns                []string
	}{
		{
			"cloud deployment show", "/api/v1/organizations/org-acme/apps/cluster-prod/web/deployments/deploy-1",
			`{"id":"deploy-1","git":{"sha":null,"repository":{"url":"https://example.test/repo","private":false}},"artifacts":[{"name":"image","metadata":{"digest":null,"cached":false}}]}`,
			[]string{"deploy-1"},
			[]string{`id:\s+deploy-1`, `git\s+sha:\s+-`, `repository\s+private:\s+no\s+url:\s+https://example\.test/repo`, `image`, `\{"cached":false,"digest":null\}`},
		},
		{
			"cloud app routes", "/api/v1/organizations/org-acme/apps/cluster-prod/web/routes",
			`{"routes":[],"synced":false,"report_state":"syncing","last_commit_at":null}`,
			nil,
			[]string{`last commit at:\s+-`, `report state:\s+syncing`, `synced:\s+no`, `routes\s+No results`},
		},
	} {
		t.Run(tc.command, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, tc.path, r.URL.Path)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			configWithIdentity(t, srv.URL)
			args := append([]string{"-o", "org-acme", "-C", "cluster-prod", "-a", "web"}, tc.args...)
			out, err := runCloudCommand(t, tc.command, args...)
			require.NoError(t, err)
			for _, pattern := range tc.patterns {
				require.Regexp(t, pattern, out.Stdout.String())
			}
		})
	}
}

func TestCloudAppContextInvoke(t *testing.T) {
	for _, tc := range []struct {
		name, envApp, wantApp, wantCluster, wantError string
		args                                          []string
	}{
		{"local config", "", "local-web", "cloud-cluster", "", []string{"-C", "cloud-cluster"}},
		{"app environment", "env-web", "env-web", "cloud-cluster", "", []string{"-C", "cloud-cluster"}},
		{"flags override environment and config", "env-web", "flag-web", "flag-cluster", "", []string{"-a", "flag-web", "-C", "flag-cluster"}},
		{"runtime cluster environment is not a Cloud ID", "env-web", "", "", "--cluster is required", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				require.Equal(t, http.MethodGet, r.Method)
				if tc.wantError == "" {
					require.Equal(t, "/api/v1/organizations/org-acme/apps/"+tc.wantCluster+"/"+tc.wantApp, r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"name":"` + tc.wantApp + `"}`))
			}))
			defer srv.Close()
			configWithIdentity(t, srv.URL)
			dir := t.TempDir()
			writeAppToml(t, dir, `name = "local-web"`)
			chdir(t, dir)
			t.Setenv("MIREN_APP", tc.envApp)
			t.Setenv("MIREN_RUNTIME_APP", "")
			t.Setenv("MIREN_CLUSTER", "env-cluster")
			d := mflags.NewDispatcher("miren")
			registerCloudCommands(d)
			cmd, ok := d.GetCommand("cloud app show").(*Cmd)
			require.True(t, ok)
			args := append([]string{"-o", "org-acme", "--json"}, tc.args...)
			require.NoError(t, cmd.fs.Parse(args))
			err := cmd.Invoke(args...)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Zero(t, requests)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, requests)
			}
		})
	}
}

func TestCloudAppDoesNotDialRuntime(t *testing.T) {
	runtime, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer runtime.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/organizations/org-acme/apps/"+runtime.LocalAddr().String()+"/web", r.URL.Path)
		_, _ = w.Write([]byte(`{"name":"web"}`))
	}))
	defer srv.Close()
	configWithIdentity(t, srv.URL)
	d := mflags.NewDispatcher("miren")
	registerCloudCommands(d)
	cmd := d.GetCommand("cloud app show").(*Cmd)
	args := []string{"-o", "org-acme", "-C", runtime.LocalAddr().String(), "-a", "web", "--json"}
	require.NoError(t, cmd.fs.Parse(args))
	require.NoError(t, cmd.Invoke(args...))
	require.NoError(t, runtime.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, _, err = runtime.ReadFrom(make([]byte, 2048))
	require.Error(t, err, "a Cloud HTTP query must not send runtime QUIC packets")
	require.True(t, err.(net.Error).Timeout())
}

func TestCloudAppMissingNameDoesNotInitialize(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MIREN_APP", "")
	d := mflags.NewDispatcher("miren")
	registerCloudCommands(d)
	cmd := d.GetCommand("cloud app show").(*Cmd)
	args := []string{"-o", "org-acme", "-C", "cloud-cluster"}
	require.NoError(t, cmd.fs.Parse(args))
	require.ErrorContains(t, cmd.Invoke(args...), "--app is required")
	require.NoDirExists(t, filepath.Join(dir, ".miren"))
}
