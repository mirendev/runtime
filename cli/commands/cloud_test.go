package commands

import (
	"encoding/json"
	"io"
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
	response = `{"keys":[{"id":17,"xid":"key-a","name":null,"key_type":"ed25519","status":"revoked","is_primary":false,"last_used_at":null}]}`
	out, err = runCloudCommand(t, "cloud account key list")
	require.NoError(t, err)
	require.Regexp(t, `key-a\s+-\s+ed25519\s+revoked\s+false\s+-`, out.Stdout.String())
	response = `{"statuses":[{"cluster_xid":"clu-a","online":true},{"cluster_xid":"clu-b","online":false}]}`
	out, err = runCloudCommand(t, "cloud cluster online-batch", "--clusters", `["clu-a","clu-b","inaccessible"]`)
	require.NoError(t, err)
	require.Regexp(t, `clu-a\s+true`, out.Stdout.String())
	require.Regexp(t, `clu-b\s+false`, out.Stdout.String())
	require.NotContains(t, out.Stdout.String(), "inaccessible")
}

func TestCloudResourceCommands(t *testing.T) {
	org := "--organization org-a"
	cluster := org + " --cluster clu-b"
	group := "--group grp-c"
	cases := []struct {
		name, method, uri, flags, body, response string
		jsonArgs                                 []string
	}{
		{"cloud me", "GET", "/api/v1/me", "", "", `{"user":{"name":"Ada"},"organizations":[]}`, nil},
		{"cloud account update", "PUT", "/api/v1/me", "--name Ada --email ada@example.test --clear-avatar", `{"name":"Ada","email":"ada@example.test","avatar_url":""}`, `{"status":"success","user":{"name":"Ada"}}`, nil},
		{"cloud account update", "PUT", "/api/v1/me", "--name Ada --email ada@example.test --avatar-url https://example.test/avatar", `{"name":"Ada","email":"ada@example.test","avatar_url":"https://example.test/avatar"}`, `{"status":"success"}`, nil},
		{"cloud account identity list", "GET", "/api/v1/identities", "", "", `{"identities":[{"id":"123","provider":"github","username":"ada"}]}`, nil},
		{"cloud account identity unlink", "DELETE", "/api/v1/identities/google/sub%2Fone%3F", "--provider google --provider-id sub/one? --force", "", "null", nil},
		{"cloud account key list", "GET", "/api/v1/users/keys", "", "", `{"keys":[{"id":17,"xid":"key-a","name":null,"last_used_at":null,"is_primary":false,"status":"revoked"}]}`, nil},
		{"cloud account key revoke", "DELETE", "/api/v1/users/keys/key-a", "--key key-a --force", "", "null", nil},
		{"cloud account token revoke-all", "POST", "/auth/revoke/user", "--force", `{"reason":""}`, `{"status":"success","message":"All user tokens revoked successfully"}`, nil},
		{"cloud account token revoke-all", "POST", "/auth/revoke/user", "--reason compromised --force", `{"reason":"compromised"}`, `{"status":"success"}`, nil},
		{"cloud invite show", "GET", "/api/v1/invitations/link%2Fone%3F", "--token link/one?", "", `{"organization_id":"org-a","expires_at":null,"already_member":false}`, nil},
		{"cloud invite accept", "POST", "/api/v1/invitations/link-token/accept", "--token link-token", "", `{"organization_id":"org-a","success":true}`, nil},
		{"cloud invite accept", "POST", "/api/v1/invitations/link-token/accept", "--token link-token", "", `{"organization_id":"org-a","already_member":true}`, nil},
		{"cloud user search", "GET", "/api/v1/users/search?q=ada%2Bops", "--query ada+ops", "", `{"users":[{"id":"usr-new","name":"Ada","email":"ada@example.test"}]}`, nil},
		{"cloud cluster delete", "DELETE", "/api/v1/organizations/org-a/clusters/clu-b", cluster + " --force", "", "null", nil},
		{"cloud cluster online-batch", "POST", "/api/v1/cluster-channel/online", "", `{"clusters":["clu-b","clu-c","inaccessible"]}`, `{"statuses":[{"cluster_xid":"clu-b","online":true},{"cluster_xid":"clu-c","online":false}]}`, []string{"--clusters", `["clu-b","clu-c","inaccessible"]`}},
		{"cloud cluster online-batch", "POST", "/api/v1/cluster-channel/online", "--clusters []", `{"clusters":[]}`, `{"statuses":[]}`, nil},
		{"cloud org show", "GET", "/api/v1/organizations/org-a", org, "", `{"organization":{"id":"org-a"},"members":[]}`, nil},
		{"cloud org member list", "GET", "/api/v1/organizations/org-a", org, "", `{"members":[{"id":"usr-d","role":"member"}]}`, nil},
		{"cloud org clusters", "GET", "/api/v1/organizations/org-a/clusters", org, "", `{"clusters":[{"id":"clu-b","node_count":null,"app_count":0}],"total":1}`, nil},
		{"cloud org create", "POST", "/api/v1/organizations", "--name new-org", `{"name":"new-org"}`, `{"organization":{"id":"org-new","role":"admin"}}`, nil},
		{"cloud org member add", "POST", "/api/v1/organizations/org-a/members", org + " --user usr-d", `{"user_id":"usr-d"}`, `{"user_id":"usr-d","role":"member"}`, nil},
		{"cloud org member set-role", "PUT", "/api/v1/organizations/org-a/users/usr-d/role", org + " --user usr-d --role admin", `{"role":"admin"}`, `{"role":"admin"}`, nil},
		{"cloud org member remove", "DELETE", "/api/v1/organizations/org-a/members/usr-d", org + " --user usr-d --force", "", "null", nil},
		{"cloud org user-search", "GET", "/api/v1/organizations/org-a/users/search?q=ada%2Bops", org + " --query ada+ops", "", `{"status":"success","users":[]}`, nil},
		{"cloud cluster show", "GET", "/api/v1/organizations/org-a/clusters/clu-b", cluster, "", `{"id":"clu-b","resources":null}`, nil},
		{"cloud cluster nodes", "GET", "/api/v1/organizations/org-a/clusters/clu-b/nodes", cluster, "", `{"nodes":[],"synced":false,"last_commit_at":null}`, nil},
		{"cloud cluster routes", "GET", "/api/v1/organizations/org-a/clusters/clu-b/routes", cluster, "", `{"routes":[],"synced":false,"report_state":"syncing"}`, nil},
		{"cloud cluster server", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server", cluster, "", `{"available":false,"connected":false}`, nil},
		{"cloud cluster upgrade-policy", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server/upgrade-policy", cluster, "", `{"enabled":false,"channel":"stable"}`, nil},
		{"cloud cluster online", "GET", "/api/v1/cluster-channel/clusters/clu-b/online", "--cluster clu-b", "", `{"cluster_xid":"clu-b","online":false}`, nil},
		{"cloud cluster update", "PATCH", "/api/v1/organizations/org-a/clusters/clu-b", cluster + " --anywhere-mode never --clear-description", `{"description":"","tags":{"region":"west"},"miren_anywhere_mode":"never"}`, `{"message":"Cluster updated successfully"}`, []string{"--tags", `{"region":"west"}`}},
		{"cloud cluster update", "PATCH", "/api/v1/organizations/org-a/clusters/clu-b", cluster, `{"description":"Production \"west\""}`, `{"message":"Cluster updated successfully"}`, []string{"--description", `Production "west"`}},
		{"cloud cluster operations", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server/operations?limit=7", cluster + " --limit 7", "", `{"operations":[{"id":"op-e","status":"failed"}]}`, nil},
		{"cloud cluster operation", "GET", "/api/v1/organizations/org-a/clusters/clu-b/server/operations/op-e", cluster + " --operation op-e", "", `{"id":"op-e","status":"failed","error":"upgrade failed"}`, nil},
		{"cloud group list", "GET", "/api/v1/organizations/org-a/groups", org, "", `{"groups":[{"id":"grp-c","is_default":false,"member_count":0}]}`, nil},
		{"cloud group show", "GET", "/api/v1/groups/grp-c", group, "", `{"group":{"id":"grp-c"}}`, nil},
		{"cloud group member list", "GET", "/api/v1/groups/grp-c/members", group, "", `{"members":[{"user_id":"usr-d"}]}`, nil},
		{"cloud group create", "POST", "/api/v1/organizations/org-a/groups", org + " --name ops --description Operators", `{"name":"ops","description":"Operators"}`, `{"group":{"id":"grp-c","name":"ops"}}`, nil},
		{"cloud group update", "PUT", "/api/v1/groups/grp-c", group + " --name operations --clear-description", `{"name":"operations","description":""}`, `{"group":{"id":"grp-c","name":"operations"}}`, nil},
		{"cloud group member add", "POST", "/api/v1/groups/grp-c/members", group + " --user usr-d", `{"user_xid":"usr-d"}`, `{"member":{"user_id":"usr-d"}}`, nil},
		{"cloud group member remove", "DELETE", "/api/v1/groups/grp-c/members/usr-d", group + " --user usr-d --force", "", `{"status":"success","message":"removed"}`, nil},
		{"cloud group delete", "DELETE", "/api/v1/groups/grp-c", group + " --force", "", "null", nil},
		{"cloud invite list", "GET", "/api/v1/organizations/org-a/invite-links", org, "", `{"invite_links":[]}`, nil},
		{"cloud invite create", "POST", "/api/v1/organizations/org-a/invite-link", org + " --expires-in 30d --max-uses 3", `{"expires_in":"30d","max_uses":3,"groups":["grp-c","grp-f"]}`, `{"id":"inv-g","invite_link":"/invite/secret"}`, []string{"--groups", `["grp-c","grp-f"]`}},
		{"cloud invite update", "PATCH", "/api/v1/organizations/org-a/invite-links/inv-g", org + " --invite inv-g --max-uses null --groups []", `{"max_uses":null,"groups":[]}`, "null", nil},
		{"cloud invite update", "PATCH", "/api/v1/organizations/org-a/invite-links/inv-g", org + " --invite inv-g --expires-in 1d", `{"expires_in":"1d"}`, "null", nil},
		{"cloud invite url", "GET", "/api/v1/organizations/org-a/invite-links/inv-g/url", org + " --invite inv-g", "", `{"invite_link":"/invite/secret"}`, nil},
		{"cloud invite revoke", "DELETE", "/api/v1/organizations/org-a/invite-links/inv-g", org + " --invite inv-g --force", "", "null", nil},
		{"cloud enroll-token create", "POST", "/api/v1/organizations/org-a/enroll-tokens", org + " --ttl-hours 12", `{"ttl_hours":12,"default_tags":{"region":"west"}}`, `{"id":"enr-h","token":"secret","cloud_config":"bootstrap"}`, []string{"--default-tags", `{"region":"west"}`}},
		{"cloud enroll-token create", "POST", "/api/v1/organizations/org-a/enroll-tokens", org, "", `{"id":"enr-h","token":"secret","cloud_config":"bootstrap"}`, nil},
		{"cloud enroll-token list", "GET", "/api/v1/organizations/org-a/enroll-tokens?limit=9&offset=13", org + " --limit 9 --offset 13", "", `{"enroll_tokens":[{"id":"enr-h","state":"pending"}]}`, nil},
		{"cloud enroll-token show", "GET", "/api/v1/organizations/org-a/enroll-tokens/enr-h", org + " --token enr-h", "", `{"id":"enr-h","state":"pending"}`, nil},
		{"cloud enroll-token revoke", "DELETE", "/api/v1/organizations/org-a/enroll-tokens/enr-h", org + " --token enr-h --force", "", "null", nil},
		{"cloud subdomain domains", "GET", "/api/v1/subdomains/base-domains", "", "", `{"domains":[{"domain":"run.garden","enabled":true}]}`, nil},
		{"cloud subdomain list", "GET", "/api/v1/organizations/org-a/subdomains", org, "", `{"subdomains":[{"id":"sub-i","assignment":null}],"total":1}`, nil},
		{"cloud subdomain claim", "POST", "/api/v1/organizations/org-a/subdomains", org + " --name hello --base-domain run.garden", `{"name":"hello","base_domain":"run.garden"}`, `{"id":"sub-i","full_hostname":"hello.run.garden"}`, nil},
		{"cloud subdomain assign", "POST", "/api/v1/organizations/org-a/subdomains/sub-i/assign", org + " --subdomain sub-i --cluster clu-b", `{"cluster_id":"clu-b"}`, `{"cluster_id":"clu-b","status":"pending_dns"}`, nil},
		{"cloud subdomain unassign", "POST", "/api/v1/organizations/org-a/subdomains/sub-i/unassign", org + " --subdomain sub-i --force", "", `{"message":"Subdomain unassigned successfully"}`, nil},
		{"cloud subdomain release", "DELETE", "/api/v1/organizations/org-a/subdomains/sub-i", org + " --subdomain sub-i --force", "", "null", nil},
		{"cloud volume list", "GET", "/api/v1/organizations/org-a/volumes?cursor=next%2Bpage&limit=11", org + " --cursor next+page --limit 11", "", `{"volumes":[],"next_cursor":"page-3"}`, nil},
		{"cloud volume updates", "GET", "/api/v1/organizations/org-a/volumes/vol-j/updates?cursor=snapshot%3Akey%2Fone&kind=snapshot&limit=17", org + " --volume vol-j --cursor snapshot:key/one --kind snapshot --limit 17", "", `{"updates":[],"next_cursor":"snapshot:key-two"}`, nil},
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
		{"cloud invite update", []string{"--organization", "org", "--invite", "inv", "--groups", `[`}, "valid JSON"},
		{"cloud invite update", []string{"--organization", "org", "--invite", "inv", "--max-uses", `3 null`}, "exactly one JSON value"},
		{"cloud org member set-role", []string{"--organization", "org", "--user", "user", "--role", "owner"}, "admin"},
		{"cloud org member add", []string{"--organization", "org"}, "--user is required"},
		{"cloud subdomain assign", []string{"--organization", "org", "--subdomain", "sub"}, "--cluster is required"},
		{"cloud org member remove", []string{"--organization", "org", "--user", "user"}, "--force"},
		{"cloud group member remove", []string{"--group", "group", "--user", "user"}, "--force"},
		{"cloud group delete", []string{"--group", "group"}, "--force"},
		{"cloud invite revoke", []string{"--organization", "org", "--invite", "invite"}, "--force"},
		{"cloud enroll-token revoke", []string{"--organization", "org", "--token", "token"}, "--force"},
		{"cloud subdomain unassign", []string{"--organization", "org", "--subdomain", "sub"}, "--force"},
		{"cloud subdomain release", []string{"--organization", "org", "--subdomain", "sub"}, "--force"},
		{"cloud account update", nil, "at least one field"},
		{"cloud group update", []string{"--group", "group"}, "at least one field"},
		{"cloud account update", []string{"--avatar-url", "https://example.test/avatar", "--clear-avatar"}, "conflicts"},
		{"cloud group update", []string{"--group", "group", "--description", "text", "--clear-description"}, "conflicts"},
		{"cloud cluster update", []string{"--organization", "org", "--cluster", "cluster", "--description", "text", "--clear-description"}, "conflicts"},
		{"cloud user search", nil, "--query is required"},
		{"cloud account identity unlink", []string{"--provider", "github", "--provider-id", "123"}, "--force"},
		{"cloud account key revoke", []string{"--key", "key"}, "--force"},
		{"cloud account token revoke-all", nil, "--force"},
		{"cloud invite accept", nil, "--token is required"},
		{"cloud cluster delete", []string{"--organization", "org", "--cluster", "cluster"}, "--force"},
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
		{"cloud account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test","avatar_url":"https://example.test/old"}`, `{"name":"New","email":"old@example.test","avatar_url":"https://example.test/old"}`, []string{"--name", "New"}, false},
		{"cloud account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test","avatar_url":"https://example.test/old"}`, `{"name":"Old","email":"old@example.test","avatar_url":""}`, []string{"--clear-avatar"}, false},
		{"cloud account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test"}`, `{"name":"Old","email":"new@example.test","avatar_url":""}`, []string{"--email", "new@example.test"}, false},
		{"cloud account update", "/api/v1/me", "user", `{"name":"Old","email":"old@example.test","avatar_url":null}`, `{"name":"New","email":"old@example.test","avatar_url":""}`, []string{"--name", "New"}, false},
		{"cloud group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Oncall","description":"Production access"}`, []string{"--group", "grp-c", "--name", "Oncall"}, false},
		{"cloud group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Operators","description":"Staging access"}`, []string{"--group", "grp-c", "--description", "Staging access"}, false},
		{"cloud group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Operators","description":""}`, []string{"--group", "grp-c", "--clear-description"}, false},
		{"cloud group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators"}`, `{"name":"Oncall","description":""}`, []string{"--group", "grp-c", "--name", "Oncall"}, false},
		{"cloud group update", "/api/v1/groups/grp-c", "group", `{"name":"Operators","description":"Production access"}`, `{"name":"Operators","description":"Production access"}`, []string{"--group", "grp-c", "--name", "Oncall"}, true},
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
		{"cloud account update", "user", []string{"--name", "New"}},
		{"cloud group update", "group", []string{"--group", "grp-c", "--name", "New"}},
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
	out, err := runCloudCommand(t, "cloud volume list", "--organization", "org")
	require.NoError(t, err)
	require.Regexp(t, `vol-j\s+data\s+9007199254740993\s+-`, out.Stdout.String())
	require.Contains(t, out.Stdout.String(), "Next cursor: page-2")
	response = `{"volumes":[],"next_cursor":"page-3"}`
	out, err = runCloudCommand(t, "cloud volume list", "--organization", "org")
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "No results")
	require.Contains(t, out.Stdout.String(), "Next cursor: page-3")
}
