package cloudauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/uplink"
)

// unreachableCloud refuses connections at once, so the signing-key refresher
// every RPCAuthenticator starts never reaches the real miren.cloud from a test.
const unreachableCloud = "http://127.0.0.1:1"

func authorizationSession(id string) uplink.Session {
	return uplink.Session{ID: id, OrganizationID: "org-1", Capabilities: []uplink.CapabilitySelection{{Name: AuthorizationCapability, Version: 1}}}
}

func authorizationSnapshot(session string, revision uint64) AuthorizationSnapshot {
	return AuthorizationSnapshot{
		SessionID: session, OrganizationID: "org-1", Revision: revision,
		Policy:      rbac.Policy{Rules: []rbac.Rule{{Name: "read apps", Groups: []string{"readers"}, Permissions: []rbac.Permission{{Resource: "apps/*", Actions: []string{"read"}}}}}},
		Memberships: map[string][]string{"alice": {"z-other", "readers"}, "bob": {}},
	}
}

func deliverSnapshot(t *testing.T, state *AuthorizationState, snapshot AuthorizationSnapshot) {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, state.receiveSnapshot(t.Context(), raw))
}

// lockedBuffer collects logs written from the session-end goroutine as well as
// the test's own.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAuthorizationDenialDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
	}{
		{"startup", "not_synced"},
		{"awaiting snapshot", "not_synced"},
		{"disconnected", ""},
		{"reconnected", ""},
		{"malformed snapshot", ""},
		{"unknown principal", "unknown_principal"},
		{"empty groups", "no_matching_rule"},
		{"unmatched action", "no_matching_rule"},
		{"allowed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs lockedBuffer
			a, err := NewRPCAuthenticator(t.Context(), Config{CloudURL: unreachableCloud, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.name != "startup" {
				a.authorization.beginSession(ctx, authorizationSession("session"))
				if tc.name != "awaiting snapshot" {
					deliverSnapshot(t, a.authorization, authorizationSnapshot("session", 1))
				}
			}
			subject, action := "alice", "read"
			switch tc.name {
			case "disconnected":
				cancel()
			case "reconnected":
				a.authorization.beginSession(ctx, authorizationSession("second"))
			case "malformed snapshot":
				require.Error(t, a.authorization.receiveSnapshot(ctx, json.RawMessage(`{`)))
			case "unknown principal":
				subject = "missing"
			case "empty groups":
				subject = "bob"
			case "unmatched action":
				action = "write"
			}
			decision, reason := a.authorization.Evaluate(&rbac.Request{Subject: subject, Resource: "apps/demo", Action: action})
			require.Equal(t, tc.reason, reason)
			logs.Reset()
			err = a.Authorize(t.Context(), &rpc.Identity{Subject: subject, Method: rpc.AuthMethodJWT, Groups: []string{"readers"}}, "apps/demo", action)
			if tc.reason == "" {
				require.Equal(t, rbac.DecisionAllow, decision)
				require.NoError(t, err)
				require.NotContains(t, logs.String(), "authorization denied")
				return
			}
			require.Equal(t, rbac.DecisionDeny, decision)
			require.Error(t, err)
			if tc.reason == "not_synced" {
				require.Contains(t, err.Error(), "cloud authorization is not synchronized")
				require.Contains(t, err.Error(), "snapshot")
			} else {
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
			}
			// The signing-key refresher logs on its own schedule, so find the
			// denial rather than assuming it was the last line.
			var record map[string]any
			for line := range bytes.SplitSeq(bytes.TrimSpace([]byte(logs.String())), []byte("\n")) {
				var candidate map[string]any
				require.NoError(t, json.Unmarshal(line, &candidate))
				if candidate["msg"] == "authorization denied" {
					record = candidate
				}
			}
			require.NotNil(t, record, "no authorization denied record")
			require.Equal(t, "WARN", record["level"])
			require.Equal(t, tc.reason, record["reason"])
		})
	}
}

func TestAuthorizationSnapshots(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default(), "", "")
	evaluate := func(subject, org string) rbac.Decision {
		decision, _ := s.Evaluate(&rbac.Request{Subject: subject, Groups: []string{"readers"}, Resource: "apps/demo", Action: "read", Context: map[string]any{"organization_id": org}})
		return decision
	}
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	ctx, cancel := context.WithCancel(t.Context())
	s.beginSession(ctx, authorizationSession("first"))
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	snapshot := authorizationSnapshot("first", 1)
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", "org-1"))
	require.Equal(t, rbac.DecisionDeny, evaluate("bob", "org-1"))
	require.Equal(t, rbac.DecisionDeny, evaluate("missing", "org-1"))
	require.Equal(t, rbac.DecisionDeny, evaluate("foreign-principal", "org-1"))
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", ""))
	require.Equal(t, []string{"z-other", "readers"}, s.snapshot.Memberships["alice"])

	// Same user/groups/request, different rules: a cached allow must be revoked.
	snapshot.Revision++
	snapshot.Policy.Rules = []rbac.Rule{}
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	snapshot = authorizationSnapshot("first", 3)
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", "org-1"))
	snapshot.Revision++
	snapshot.Memberships["alice"] = []string{}
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	snapshot = authorizationSnapshot("first", 5)
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", "org-1"))
	snapshot.Revision++
	delete(snapshot.Memberships, "alice")
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))

	deliverSnapshot(t, s, authorizationSnapshot("first", 7))
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", "org-1"))
	// Losing cloud is not news about permissions: the last snapshot stands
	// through the disconnect and the reconnect, until cloud sends another.
	cancel()
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", "org-1"))
	s.beginSession(t.Context(), authorizationSession("second"))
	require.Equal(t, rbac.DecisionAllow, evaluate("alice", "org-1"))
	require.Equal(t, rbac.DecisionDeny, evaluate("bob", "org-1"))
	// Reconnect resets the revision and receives changes missed while offline.
	snapshot = authorizationSnapshot("second", 1)
	snapshot.Memberships = map[string][]string{"bob": {"readers"}}
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	require.Equal(t, rbac.DecisionAllow, evaluate("bob", "org-1"))
}

func TestAuthorizationLocalTagSelectorsAndEmptyState(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default(), "", "")
	s.beginSession(t.Context(), authorizationSession("session"))
	snapshot := authorizationSnapshot("session", 1)
	snapshot.Policy.Rules[0].TagSelector.Expressions = []rbac.TagExpression{{Tag: "environment", Operator: "equals", Value: "production"}}
	deliverSnapshot(t, s, snapshot)
	for _, tc := range []struct {
		environment string
		want        rbac.Decision
	}{
		{"production", rbac.DecisionAllow},
		{"development", rbac.DecisionDeny},
	} {
		decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read", Tags: map[string]any{"environment": tc.environment}})
		require.Equal(t, tc.want, decision)
	}
	snapshot.Revision++
	snapshot.Policy.Rules = []rbac.Rule{}
	snapshot.Memberships = map[string][]string{}
	deliverSnapshot(t, s, snapshot)
	decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read", Tags: map[string]any{"environment": "production"}})
	require.Equal(t, rbac.DecisionDeny, decision)
}

func TestAuthorizationRejectsInvalidSnapshots(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default(), "", "")
	s.beginSession(t.Context(), authorizationSession("session"))
	deliverSnapshot(t, s, authorizationSnapshot("session", 2))
	for _, tc := range []struct {
		name   string
		mutate func(*AuthorizationSnapshot)
	}{
		{"wrong session", func(s *AuthorizationSnapshot) { s.SessionID = "old" }},
		{"wrong organization", func(s *AuthorizationSnapshot) { s.OrganizationID = "other" }},
		{"old revision", func(s *AuthorizationSnapshot) { s.Revision = 1 }},
		{"duplicate", func(s *AuthorizationSnapshot) { s.Revision = 2 }},
		{"zero revision", func(s *AuthorizationSnapshot) { s.Revision = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := authorizationSnapshot("session", 3)
			tc.mutate(&snapshot)
			raw, err := json.Marshal(snapshot)
			require.NoError(t, err)
			require.Error(t, s.receiveSnapshot(t.Context(), raw))
			require.Equal(t, uint64(2), s.snapshot.Revision)
			decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
			require.Equal(t, rbac.DecisionAllow, decision)
		})
	}
	require.Error(t, s.receiveSnapshot(t.Context(), json.RawMessage(`{`)))
	s.beginSession(t.Context(), uplink.Session{ID: "unselected", OrganizationID: "org-1"})
	raw, err := json.Marshal(authorizationSnapshot("unselected", 1))
	require.NoError(t, err)
	require.Error(t, s.receiveSnapshot(t.Context(), raw))
}

func TestMalformedAuthorizationSnapshotKeepsAccess(t *testing.T) {
	for _, body := range []string{
		`{"session_id":"session","organization_id":"org-1","revision":3,"policy":{"rules":null},"memberships":{}}`,
		`{"session_id":"session","organization_id":"org-1","revision":3,"policy":{"rules":[]},"memberships":null}`,
		`{"session_id":"session","organization_id":"org-1","revision":3}`,
		`{"session_id":"session","organization_id":"org-1","revision":3,"policy":{"rules":"invalid"},"memberships":{}}`,
		`{`,
	} {
		t.Run(body, func(t *testing.T) {
			s := NewAuthorizationState(t.Context(), slog.Default(), "", "")
			s.beginSession(t.Context(), authorizationSession("session"))
			deliverSnapshot(t, s, authorizationSnapshot("session", 2))
			evaluate := func() rbac.Decision {
				decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
				return decision
			}
			require.Equal(t, rbac.DecisionAllow, evaluate())
			require.Error(t, s.receiveSnapshot(t.Context(), json.RawMessage(body)))
			require.Equal(t, rbac.DecisionAllow, evaluate())
			stale, err := json.Marshal(authorizationSnapshot("session", 2))
			require.NoError(t, err)
			require.Error(t, s.receiveSnapshot(t.Context(), stale))
			revoked := authorizationSnapshot("session", 4)
			revoked.Memberships["alice"] = []string{}
			deliverSnapshot(t, s, revoked)
			require.Equal(t, rbac.DecisionDeny, evaluate())
		})
	}
}

func TestAuthorizationConcurrentRevocation(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default(), "", "")
	s.beginSession(t.Context(), authorizationSession("session"))
	deliverSnapshot(t, s, authorizationSnapshot("session", 1))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
			}
		})
	}
	snapshot := authorizationSnapshot("session", 2)
	snapshot.Policy.Rules = []rbac.Rule{}
	deliverSnapshot(t, s, snapshot)
	wg.Wait()
	for range 100 {
		decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
		require.Equal(t, rbac.DecisionDeny, decision)
	}
}

func TestRPCUsesPushedMembershipsWithoutHTTP(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The signing-key refresher fetches JWKS on its own schedule; what
		// must not happen is a policy or membership fetch.
		if r.URL.Path != "/.well-known/jwks.json" {
			requests.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	a, err := NewRPCAuthenticator(t.Context(), Config{CloudURL: srv.URL, Logger: slog.Default()})
	require.NoError(t, err)
	a.authorization.beginSession(t.Context(), authorizationSession("session"))
	deliverSnapshot(t, a.authorization, authorizationSnapshot("session", 1))
	// A validated cached token can have stale groups; only its identity matters.
	a.tokenCache.Set("cached-token", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "alice"}, OrganizationID: "org-1", GroupIDs: []string{"not-readers"}})
	identity, err := a.Authenticate(t.Context(), &rpc.Credentials{Authorization: "Bearer cached-token"})
	require.NoError(t, err)
	require.Equal(t, []string{"not-readers"}, identity.Groups)
	require.NoError(t, a.Authorize(t.Context(), identity, "apps/demo", "read"))
	snapshot := authorizationSnapshot("session", 2)
	snapshot.Memberships["alice"] = []string{}
	deliverSnapshot(t, a.authorization, snapshot)
	identity.Groups = []string{"readers"}
	require.Error(t, a.Authorize(t.Context(), identity, "apps/demo", "read"))
	require.NoError(t, a.Authorize(t.Context(), &rpc.Identity{Method: rpc.AuthMethodCert}, "apps/demo", "read"))
	require.Zero(t, requests.Load(), "startup and authorization must not fetch cloud state")
}

func TestRPCServiceAccountPushedMemberships(t *testing.T) {
	a, err := NewRPCAuthenticator(t.Context(), Config{CloudURL: unreachableCloud, Logger: slog.Default()})
	require.NoError(t, err)
	a.authorization.beginSession(t.Context(), authorizationSession("session"))
	snapshot := authorizationSnapshot("session", 1)
	snapshot.Memberships["svc-automation"] = []string{"readers"}
	deliverSnapshot(t, a.authorization, snapshot)
	a.tokenCache.Set("service-token", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "svc-automation"}, OrganizationID: "org-1", GroupIDs: []string{"stale-group"}})
	identity, err := a.Authenticate(t.Context(), &rpc.Credentials{Authorization: "Bearer service-token"})
	require.NoError(t, err)
	require.Equal(t, "svc-automation", identity.Subject)
	require.Equal(t, rpc.AuthMethodJWT, identity.Method)
	require.NoError(t, a.Authorize(t.Context(), identity, "apps/demo", "read"))
	snapshot.Revision++
	snapshot.Memberships["svc-automation"] = []string{}
	deliverSnapshot(t, a.authorization, snapshot)
	identity.Groups = []string{"readers"}
	require.Error(t, a.Authorize(t.Context(), identity, "apps/demo", "read"))
	snapshot.Revision++
	delete(snapshot.Memberships, "svc-automation")
	deliverSnapshot(t, a.authorization, snapshot)
	require.Error(t, a.Authorize(t.Context(), identity, "apps/demo", "read"))
}

func TestRPCCloudTokenOrganizationFormats(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/.well-known/jwks.json", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(auth.JWKS{Keys: []auth.JWK{{Kty: "OKP", Crv: "Ed25519", Kid: "cloud-key", X: base64.RawURLEncoding.EncodeToString(publicKey)}}}))
	}))
	defer srv.Close()
	a, err := NewRPCAuthenticator(t.Context(), Config{CloudURL: srv.URL, Logger: slog.Default()})
	require.NoError(t, err)
	a.authorization.beginSession(t.Context(), authorizationSession("session"))
	snapshot := authorizationSnapshot("session", 1)
	snapshot.Memberships["svc-automation"] = []string{"readers"}
	snapshot.Memberships["usr-current"] = []string{"readers"}
	deliverSnapshot(t, a.authorization, snapshot)
	for _, tc := range []struct {
		name    string
		claims  jwt.MapClaims
		allowed bool
	}{
		{"user without organization", jwt.MapClaims{"sub": "usr-current"}, true},
		{"service account numeric organization", jwt.MapClaims{"sub": "svc-automation", "organization_id": 42, "group_ids": []string{"stale-group"}}, true},
		{"foreign user", jwt.MapClaims{"sub": "usr-foreign", "group_ids": []string{"readers"}}, false},
		{"foreign service account", jwt.MapClaims{"sub": "svc-foreign", "organization_id": 43, "group_ids": []string{"readers"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, tc.claims)
			token.Header["kid"] = "cloud-key"
			raw, err := token.SignedString(privateKey)
			require.NoError(t, err)
			identity, err := a.Authenticate(t.Context(), &rpc.Credentials{Authorization: "Bearer " + raw})
			require.NoError(t, err)
			require.NotNil(t, identity)
			err = a.Authorize(t.Context(), identity, "apps/demo", "read")
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCloudEmittedAuthorizationEnvelope(t *testing.T) {
	// Captured from mirendev/cloud's real PostgreSQL + WebSocket emitter test
	// for MIR-2005, rather than marshaled from the runtime's own wire types.
	raw, err := os.ReadFile("testdata/authorization.snapshot.json")
	require.NoError(t, err)
	var envelope uplink.Envelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	for _, environment := range []string{"production", "development"} {
		t.Run(environment, func(t *testing.T) {
			a, err := NewRPCAuthenticator(t.Context(), Config{CloudURL: unreachableCloud, Logger: slog.Default(), Tags: map[string]any{"environment": environment}})
			require.NoError(t, err)
			router := uplink.NewMessageRouter()
			link := uplink.NewClient("https://unused.invalid", nil, router, slog.Default())
			a.RegisterAuthorization(link)
			session := authorizationSession("6779202f-d9c4-45d4-adbc-11623faf26c2")
			session.OrganizationID = "org-gd9ktl4kpl33"
			a.authorization.beginSession(t.Context(), session)
			require.NoError(t, router.Dispatch(t.Context(), envelope))
			identity := &rpc.Identity{Subject: "usr-l0668v4hhgcw", Method: rpc.AuthMethodJWT, Metadata: map[string]any{"organization_id": "org-gd9ktl4kpl33"}}
			for _, action := range []string{"read", "write", "delete"} {
				err := a.Authorize(t.Context(), identity, "apps/demo", action)
				if environment == "production" && action != "delete" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}

func TestAuthorizationSnapshotSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), AuthorizationSnapshotFile)
	evaluate := func(s *AuthorizationState, subject string) rbac.Decision {
		decision, _ := s.Evaluate(&rbac.Request{Subject: subject, Resource: "apps/demo", Action: "read"})
		return decision
	}
	first := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	first.beginSession(t.Context(), authorizationSession("first"))
	deliverSnapshot(t, first, authorizationSnapshot("first", 1))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// A restart before cloud is reachable authorizes from the saved snapshot.
	restarted := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	require.Equal(t, rbac.DecisionAllow, evaluate(restarted, "alice"))

	// Reconnecting keeps it until the new session's first snapshot replaces it,
	// whatever revision that session starts from.
	restarted.beginSession(t.Context(), authorizationSession("second"))
	require.Equal(t, rbac.DecisionAllow, evaluate(restarted, "alice"))
	replacement := authorizationSnapshot("second", 1)
	replacement.Memberships = map[string][]string{"bob": {"readers"}}
	deliverSnapshot(t, restarted, replacement)
	require.Equal(t, rbac.DecisionDeny, evaluate(restarted, "alice"))
	require.Equal(t, rbac.DecisionAllow, evaluate(NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1"), "bob"))
}

func TestAuthorizationSnapshotFromAnotherOrganizationIsDiscarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), AuthorizationSnapshotFile)
	s := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	s.beginSession(t.Context(), authorizationSession("first"))
	deliverSnapshot(t, s, authorizationSnapshot("first", 1))
	session := authorizationSession("second")
	session.OrganizationID = "org-2"
	s.beginSession(t.Context(), session)
	decision, reason := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
	require.Equal(t, rbac.DecisionDeny, decision)
	require.Equal(t, "not_synced", reason)
	require.NoFileExists(t, path)
}

func TestUnreadableSavedAuthorizationSnapshotIsIgnored(t *testing.T) {
	for _, body := range []string{`{`, `{"session_id":"s","organization_id":"org-1","revision":1}`, `"not a snapshot"`} {
		path := filepath.Join(t.TempDir(), AuthorizationSnapshotFile)
		require.NoError(t, os.WriteFile(path, []byte(`{"cluster_id":"cluster-1","snapshot":`+body+`}`), 0600))
		s := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
		_, reason := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
		require.Equal(t, "not_synced", reason)
	}
}

// A server registered again gets a new cluster ID, and must not restore the
// previous registration's permissions even if the old process wrote them late.
func TestSavedAuthorizationSnapshotBelongsToItsCluster(t *testing.T) {
	path := filepath.Join(t.TempDir(), AuthorizationSnapshotFile)
	old := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	old.beginSession(t.Context(), authorizationSession("first"))
	deliverSnapshot(t, old, authorizationSnapshot("first", 1))
	reregistered := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-2")
	_, reason := reregistered.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
	require.Equal(t, "not_synced", reason)
}

func TestScopedAuthorizationSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), AuthorizationSnapshotFile)
	state := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	router := uplink.NewMessageRouter()
	link := uplink.NewClient("https://unused.invalid", nil, router, slog.Default())
	state.Register(link)
	state.beginSession(t.Context(), authorizationSession("session"))
	dispatch := func(revision int, permissions string) error {
		return router.Dispatch(t.Context(), uplink.Envelope{Type: TypeAuthorizationSnapshot, Data: json.RawMessage(fmt.Sprintf(`{
			"session_id":"session", "organization_id":"org-1", "revision":%d,
			"policy":{"rules":[{"groups":["members"],"permissions":%s}]},
			"memberships":{"alice":["members"]}
		}`, revision, permissions))})
	}
	evaluate := func(s *AuthorizationState, instance, owner string) rbac.Decision {
		decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "app", Action: "list", Instance: instance, Owner: owner})
		return decision
	}
	require.NoError(t, dispatch(1, `[{"resource":"app","actions":["list"],"scope":"$owner"}]`))
	require.Equal(t, rbac.DecisionAllow, evaluate(state, "alice-blog", "alice"))
	require.Equal(t, rbac.DecisionDeny, evaluate(state, "bob-blog", "bob"))
	require.Equal(t, rbac.DecisionDeny, evaluate(state, "alice-blog", "bob"))
	restarted := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	require.Equal(t, rbac.DecisionAllow, evaluate(restarted, "alice-blog", "alice"))
	require.Equal(t, rbac.DecisionDeny, evaluate(restarted, "bob-blog", "bob"))

	// A replacement withdraws even a cached ownership grant, and persists the
	// explicit collaborator scope instead of silently dropping it on restart.
	require.NoError(t, dispatch(2, `[{"resource":"app","actions":["list"],"scope":"shared-*"}]`))
	require.Equal(t, rbac.DecisionDeny, evaluate(state, "alice-blog", "alice"))
	require.Equal(t, rbac.DecisionAllow, evaluate(state, "shared-blog", "bob"))
	restarted = NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	require.Equal(t, rbac.DecisionDeny, evaluate(restarted, "alice-blog", "alice"))
	require.Equal(t, rbac.DecisionAllow, evaluate(restarted, "shared-blog", "bob"))

	for i, scope := range []string{`null`, `42`, `{}`, `[]`} {
		require.Error(t, dispatch(3+i, `[{"resource":"app","actions":["list"],"scope":`+scope+`}]`))
		require.Equal(t, rbac.DecisionDeny, evaluate(state, "alice-blog", "alice"))
		require.Equal(t, rbac.DecisionAllow, evaluate(state, "shared-blog", "bob"))
	}
	// Unknown special scopes and malformed globs grant nothing, not everything.
	require.NoError(t, dispatch(7, `[{"resource":"app","actions":["list"],"scope":"$future"},{"resource":"app","actions":["list"],"scope":"["}]`))
	require.Equal(t, rbac.DecisionDeny, evaluate(state, "$future", "alice"))
	require.Equal(t, rbac.DecisionDeny, evaluate(state, "[", "alice"))
	require.Equal(t, rbac.DecisionDeny, evaluate(state, "shared-blog", "bob"))

	// Old Cloud payloads omitting scope retain their unscoped behavior.
	require.NoError(t, dispatch(8, `[{"resource":"app","actions":["list"]}]`))
	require.Equal(t, rbac.DecisionAllow, evaluate(state, "bob-blog", "bob"))
	require.Equal(t, rbac.DecisionAllow, evaluate(state, "", ""))
}

func TestResolvedAppAuthorizationVersions(t *testing.T) {
	for _, version := range []uint{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "authorization.json")
			a := &RPCAuthenticator{logger: slog.Default(), authorization: NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")}
			session := authorizationSession("session")
			session.Capabilities[0].Version = version
			a.authorization.beginSession(t.Context(), session)
			snapshot := authorizationSnapshot("session", 1)
			snapshot.Version = version
			permission := rbac.Permission{Resource: "app", Actions: []string{"read"}, Scope: "$owner"}
			if version == 1 {
				permission = rbac.Permission{Resource: "crud", Actions: []string{"getconfiguration"}}
			}
			snapshot.Policy.Rules[0].Permissions = []rbac.Permission{permission}
			deliverSnapshot(t, a.authorization, snapshot)
			for _, restarted := range []bool{false, true} {
				if restarted {
					a.authorization = NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
				}
				identity := &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT}
				ctx := rpc.ContextWithIdentity(t.Context(), identity)
				ctx = rpc.ContextWithAuthorization(ctx, a, "crud", "getconfiguration")
				require.NoError(t, a.Authorize(ctx, identity, "crud", "getconfiguration"))
				request := rpc.ResourceRequest{Resource: "app", Action: "read", Instance: "blog", Owner: "alice"}
				require.NoError(t, rpc.AuthorizeResource(ctx, request))
				request.Owner = "bob"
				if version == 2 {
					require.ErrorIs(t, rpc.AuthorizeResource(ctx, request), rpc.ErrUnauthorized)
				} else {
					require.NoError(t, rpc.AuthorizeResource(ctx, request), "legacy unscoped grant must retain access")
				}
				request.Owner = "alice"
				request.Action = "configure"
				ctx = rpc.ContextWithAuthorization(ctx, a, "crud", "setconfiguration")
				require.ErrorIs(t, rpc.AuthorizeResource(ctx, request), rpc.ErrUnauthorized)
				identity.Subject = "missing"
				ctx = rpc.ContextWithAuthorization(ctx, a, "crud", "getconfiguration")
				request.Action = "read"
				require.ErrorIs(t, rpc.AuthorizeResource(ctx, request), rpc.ErrUnauthorized)
			}
		})
	}
}

func TestInventoryAuthorizationFailuresAndQuietExclusions(t *testing.T) {
	var logs lockedBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	a := &RPCAuthenticator{logger: logger, authorization: NewAuthorizationState(t.Context(), logger, "", "")}
	identity := &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT}
	ctx := rpc.ContextWithIdentity(t.Context(), identity)
	ctx = rpc.ContextWithAuthorization(ctx, a, "crud", "list")
	request := rpc.ResourceRequest{Resource: "app", Action: "list"}
	_, err := rpc.FilterResource(ctx, request)
	require.ErrorContains(t, err, "not synchronized")
	require.Contains(t, logs.String(), `"level":"WARN"`)

	session := authorizationSession("session")
	session.Capabilities[0].Version = 2
	a.authorization.beginSession(t.Context(), session)
	snapshot := authorizationSnapshot("session", 1)
	snapshot.Version = 2
	snapshot.Policy.Rules[0].Permissions = []rbac.Permission{{Resource: "app", Actions: []string{"list"}, Scope: "$owner"}}
	deliverSnapshot(t, a.authorization, snapshot)
	logs.Reset()
	allowed, err := rpc.FilterResource(ctx, request)
	require.NoError(t, err, "collection preflight must not reject scoped users")
	require.False(t, allowed)
	request.Instance, request.Owner = "shop", "bob"
	allowed, err = rpc.FilterResource(ctx, request)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NotContains(t, logs.String(), "authorization denied", "ordinary hidden rows must not warn")
	request.Owner = "alice"
	allowed, err = rpc.FilterResource(ctx, request)
	require.NoError(t, err)
	require.True(t, allowed)
	request.Owner = "bob"
	require.ErrorIs(t, rpc.AuthorizeResource(ctx, request), rpc.ErrUnauthorized)
	require.Contains(t, logs.String(), `"level":"WARN"`, "direct denials still warn")
	logs.Reset()
	identity.Subject = "missing"
	_, err = rpc.FilterResource(ctx, request)
	require.ErrorContains(t, err, "unknown_principal")
	require.Contains(t, logs.String(), `"level":"WARN"`)
}

func TestAuthorizationSnapshotVersionMustBeNegotiated(t *testing.T) {
	for _, selected := range []uint{1, 2} {
		state := NewAuthorizationState(t.Context(), slog.Default(), "", "")
		session := authorizationSession("session")
		session.Capabilities[0].Version = selected
		state.beginSession(t.Context(), session)
		snapshot := authorizationSnapshot("session", 1)
		snapshot.Version = selected
		deliverSnapshot(t, state, snapshot)
		snapshot.Revision++
		snapshot.Version = 3 - selected
		snapshot.Policy.Rules = []rbac.Rule{}
		raw, err := json.Marshal(snapshot)
		require.NoError(t, err)
		require.ErrorContains(t, state.receiveSnapshot(t.Context(), raw), "version")
		decision, _ := state.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
		require.Equal(t, rbac.DecisionAllow, decision, "rejected snapshot must leave prior policy in effect")
	}
}

func TestLogicalCompatibilityAcrossPolicyReplacement(t *testing.T) {
	a := &RPCAuthenticator{logger: slog.Default(), authorization: NewAuthorizationState(t.Context(), slog.Default(), "", "")}
	comp := oidcauth.NewCompositeAuthorizer(a)
	identity := &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT}
	ctx := rpc.ContextWithIdentity(t.Context(), identity)
	ctx = rpc.ContextWithAuthorization(ctx, comp, "crud", "getconfiguration")
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	_, err := inmem.Client.Create(t.Context(), "mine", &core_v1alpha.App{OwnedBy: "alice"})
	require.NoError(t, err)
	_, err = inmem.Client.Create(t.Context(), "theirs", &core_v1alpha.App{OwnedBy: "bob"})
	require.NoError(t, err)
	for i, version := range []uint{1, 2, 1} {
		session := authorizationSession(fmt.Sprint(i))
		session.Capabilities[0].Version = version
		a.authorization.beginSession(t.Context(), session)
		snapshot := authorizationSnapshot(session.ID, 1)
		snapshot.Version = version
		permission := rbac.Permission{Resource: "crud", Actions: []string{"getconfiguration"}}
		if version == 2 {
			permission = rbac.Permission{Resource: "app", Actions: []string{"read"}, Scope: "$owner"}
		}
		snapshot.Policy.Rules[0].Permissions = []rbac.Permission{permission}
		deliverSnapshot(t, a.authorization, snapshot)
		require.NoError(t, comp.Authorize(ctx, identity, "crud", "getconfiguration"))
		require.Equal(t, version == 2, rpc.UsesLogicalAuthorization(ctx))
		require.NoError(t, app.Authorize(ctx, inmem.EAC, "mine", "read"))
		err = app.Authorize(ctx, inmem.EAC, "theirs", "read")
		if version == 2 {
			require.ErrorIs(t, err, rpc.ErrUnauthorized)
		} else {
			require.NoError(t, err)
		}
		// Replace a cached grant before the handler: no legacy shortcut may
		// evade the revocation, including after a v2-to-v1 replacement.
		snapshot.Revision++
		snapshot.Policy.Rules = []rbac.Rule{}
		deliverSnapshot(t, a.authorization, snapshot)
		require.True(t, rpc.UsesLogicalAuthorization(ctx), "revoked legacy method cannot skip enforcement")
		require.ErrorIs(t, app.Authorize(ctx, inmem.EAC, "mine", "read"), rpc.ErrUnauthorized)
		if version == 1 {
			require.ErrorIs(t, comp.Authorize(ctx, identity, "crud", "getconfiguration"), rpc.ErrUnauthorized)
		}
	}
}

func TestAnalyzeAppUsesMethodPolicyInBothVersions(t *testing.T) {
	require.Empty(t, rpc.AppAction("builder", "analyzeapp"), "source analysis cannot defer to an app target")
	for _, version := range []uint{1, 2} {
		a := &RPCAuthenticator{logger: slog.Default(), authorization: NewAuthorizationState(t.Context(), slog.Default(), "", "")}
		comp := oidcauth.NewCompositeAuthorizer(a)
		identity := &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT}
		session := authorizationSession("analysis")
		session.Capabilities[0].Version = version
		a.authorization.beginSession(t.Context(), session)
		snapshot := authorizationSnapshot(session.ID, 1)
		snapshot.Version = version
		snapshot.Policy.Rules[0].Permissions = []rbac.Permission{{Resource: "app", Actions: []string{"read"}}}
		deliverSnapshot(t, a.authorization, snapshot)
		require.ErrorIs(t, comp.Authorize(t.Context(), identity, "builder", "analyzeapp"), rpc.ErrUnauthorized)
		snapshot.Revision++
		snapshot.Policy.Rules[0].Permissions = []rbac.Permission{{Resource: "builder", Actions: []string{"analyzeapp"}}}
		deliverSnapshot(t, a.authorization, snapshot)
		require.NoError(t, comp.Authorize(t.Context(), identity, "builder", "analyzeapp"))
		snapshot.Revision++
		snapshot.Policy.Rules = []rbac.Rule{}
		deliverSnapshot(t, a.authorization, snapshot)
		require.ErrorIs(t, comp.Authorize(t.Context(), identity, "builder", "analyzeapp"), rpc.ErrUnauthorized)
	}
}

func TestSavedV2SurvivesRolloutHoldUntilValidReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), AuthorizationSnapshotFile)
	state := NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	session := authorizationSession("v2")
	session.Capabilities[0].Version = 2
	state.beginSession(t.Context(), session)
	snapshot := authorizationSnapshot(session.ID, 1)
	snapshot.Version = 2
	snapshot.Policy.Rules[0].Permissions = []rbac.Permission{{Resource: "app", Actions: []string{"read"}, Scope: "$owner"}}
	deliverSnapshot(t, state, snapshot)
	state = NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	a := &RPCAuthenticator{logger: slog.Default(), authorization: state}
	comp := oidcauth.NewCompositeAuthorizer(a)
	identity := &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT}
	ctx := rpc.ContextWithIdentity(t.Context(), identity)
	ctx = rpc.ContextWithAuthorization(ctx, comp, "crud", "getconfiguration")
	check := func(scoped bool) {
		t.Helper()
		require.Equal(t, scoped, rpc.UsesLogicalAuthorization(ctx))
		require.NoError(t, rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "app", Action: "read", Instance: "mine", Owner: "alice"}))
		err := rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "app", Action: "read", Instance: "theirs", Owner: "bob"})
		if scoped {
			require.ErrorIs(t, err, rpc.ErrUnauthorized)
		} else {
			require.NoError(t, err)
		}
	}
	check(true)
	state.beginSession(t.Context(), authorizationSession("v1"))
	check(true)
	snapshot.SessionID = "v1"
	snapshot.Revision = 1
	snapshot.Policy.Rules[0].Permissions = []rbac.Permission{{Resource: "app", Actions: []string{"read"}}}
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.ErrorContains(t, state.receiveSnapshot(t.Context(), raw), "version")
	check(true)
	snapshot.Version = 1
	snapshot.Revision++
	snapshot.Policy.Rules[0].Permissions = []rbac.Permission{{Resource: "crud", Actions: []string{"getconfiguration"}}}
	deliverSnapshot(t, state, snapshot)
	check(false)
	state = NewAuthorizationState(t.Context(), slog.Default(), path, "cluster-1")
	a.authorization = state
	check(false)
}
