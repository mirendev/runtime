package cloudauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/uplink"
)

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

func TestAuthorizationDenialDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
	}{
		{"startup", "not_synced"},
		{"awaiting snapshot", "not_synced"},
		{"disconnected", "not_synced"},
		{"reconnected", "not_synced"},
		{"malformed snapshot", "not_synced"},
		{"unknown principal", "unknown_principal"},
		{"empty groups", "no_matching_rule"},
		{"unmatched action", "no_matching_rule"},
		{"allowed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			a, err := NewRPCAuthenticator(t.Context(), Config{Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
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
				require.EqualError(t, err, "access denied by RBAC policy")
			}
			lines := bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n"))
			var record map[string]any
			require.NoError(t, json.Unmarshal(lines[len(lines)-1], &record))
			require.Equal(t, "authorization denied", record["msg"])
			require.Equal(t, "WARN", record["level"])
			require.Equal(t, tc.reason, record["reason"])
		})
	}
}

func TestAuthorizationSnapshots(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default())
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
	cancel()
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	s.beginSession(t.Context(), authorizationSession("second"))
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	// Reconnect resets the revision and receives changes missed while offline.
	snapshot = authorizationSnapshot("second", 1)
	snapshot.Memberships = map[string][]string{"bob": {"readers"}}
	deliverSnapshot(t, s, snapshot)
	require.Equal(t, rbac.DecisionDeny, evaluate("alice", "org-1"))
	require.Equal(t, rbac.DecisionAllow, evaluate("bob", "org-1"))
}

func TestAuthorizationLocalTagSelectorsAndEmptyState(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default())
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
	s := NewAuthorizationState(t.Context(), slog.Default())
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

func TestMalformedAuthorizationSnapshotRevokesCachedGrants(t *testing.T) {
	for _, body := range []string{
		`{"session_id":"session","organization_id":"org-1","revision":3,"policy":{"rules":null},"memberships":{}}`,
		`{"session_id":"session","organization_id":"org-1","revision":3,"policy":{"rules":[]},"memberships":null}`,
		`{"session_id":"session","organization_id":"org-1","revision":3}`,
		`{"session_id":"session","organization_id":"org-1","revision":3,"policy":{"rules":"invalid"},"memberships":{}}`,
		`{`,
	} {
		t.Run(body, func(t *testing.T) {
			s := NewAuthorizationState(t.Context(), slog.Default())
			s.beginSession(t.Context(), authorizationSession("session"))
			deliverSnapshot(t, s, authorizationSnapshot("session", 2))
			evaluate := func() rbac.Decision {
				decision, _ := s.Evaluate(&rbac.Request{Subject: "alice", Resource: "apps/demo", Action: "read"})
				return decision
			}
			require.Equal(t, rbac.DecisionAllow, evaluate())
			require.Error(t, s.receiveSnapshot(t.Context(), json.RawMessage(body)))
			require.Equal(t, rbac.DecisionDeny, evaluate())
			stale, err := json.Marshal(authorizationSnapshot("session", 2))
			require.NoError(t, err)
			require.Error(t, s.receiveSnapshot(t.Context(), stale))
			require.Equal(t, rbac.DecisionDeny, evaluate())
			deliverSnapshot(t, s, authorizationSnapshot("session", 4))
			require.Equal(t, rbac.DecisionAllow, evaluate())
		})
	}
}

func TestAuthorizationConcurrentRevocation(t *testing.T) {
	s := NewAuthorizationState(t.Context(), slog.Default())
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
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
	a, err := NewRPCAuthenticator(t.Context(), Config{Logger: slog.Default()})
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
			a, err := NewRPCAuthenticator(t.Context(), Config{Logger: slog.Default(), Tags: map[string]any{"environment": environment}})
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
