package cloudauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/uplink"
)

type authorizationTokenSource struct{}

func (authorizationTokenSource) GetToken(context.Context) (string, error) {
	return "cluster-token", nil
}

func TestAuthorizationOverUplink(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	type connection struct {
		ws *websocket.Conn
		id string
	}
	connections := make(chan connection, 4)
	var sequence atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cluster-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		var helloEnvelope uplink.Envelope
		if err := wsjson.Read(ctx, ws, &helloEnvelope); err != nil {
			return
		}
		var hello uplink.SessionHello
		if helloEnvelope.Type != uplink.TypeSessionHello || json.Unmarshal(helloEnvelope.Data, &hello) != nil {
			return
		}
		if len(hello.Capabilities) != 1 || hello.Capabilities[0].Name != AuthorizationCapability {
			return
		}
		id := fmt.Sprintf("session-%d", sequence.Add(1))
		welcome := uplink.SessionWelcome{
			HandshakeVersion: 1, SessionID: id, OrganizationID: "org-1",
			ServerReceiveTime: time.Now(), ServerTransmitTime: time.Now(),
			Capabilities: []uplink.CapabilitySelection{{Name: AuthorizationCapability, Version: 1}},
		}
		raw, err := json.Marshal(welcome)
		if err != nil || wsjson.Write(ctx, ws, uplink.Envelope{Type: uplink.TypeSessionWelcome, Data: raw}) != nil {
			return
		}
		select {
		case connections <- connection{ws, id}:
		case <-ctx.Done():
			return
		}
		_, _, _ = ws.Read(ctx)
	}))
	defer srv.Close()

	s := NewAuthorizationState(ctx, slog.Default())
	link := uplink.NewClient(srv.URL, authorizationTokenSource{}, uplink.NewMessageRouter(), slog.Default(), uplink.WithSession(uplink.SessionIdentity{RuntimeVersion: "test"}))
	s.Register(link)
	done := make(chan error, 1)
	go func() { done <- link.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	nextConnection := func() connection {
		select {
		case conn := <-connections:
			return conn
		case <-ctx.Done():
			t.Fatal("uplink did not connect")
			return connection{}
		}
	}
	send := func(conn connection, snapshot AuthorizationSnapshot) {
		raw, err := json.Marshal(snapshot)
		require.NoError(t, err)
		require.NoError(t, wsjson.Write(ctx, conn.ws, uplink.Envelope{Type: TypeAuthorizationSnapshot, Data: raw}))
	}
	decision := func(subject string) rbac.Decision {
		return s.Evaluate(&rbac.Request{Subject: subject, Resource: "apps/demo", Action: "read"})
	}
	waitDecision := func(subject string, want rbac.Decision) {
		require.Eventually(t, func() bool { return decision(subject) == want }, 3*time.Second, time.Millisecond)
	}
	conn := nextConnection()
	require.Equal(t, rbac.DecisionDeny, decision("alice"))
	send(conn, authorizationSnapshot(conn.id, 1))
	waitDecision("alice", rbac.DecisionAllow)
	snapshot := authorizationSnapshot(conn.id, 2)
	snapshot.Policy.Rules = []rbac.Rule{}
	send(conn, snapshot)
	waitDecision("alice", rbac.DecisionDeny)
	send(conn, authorizationSnapshot(conn.id, 3))
	waitDecision("alice", rbac.DecisionAllow)
	snapshot = authorizationSnapshot(conn.id, 4)
	snapshot.Memberships["alice"] = []string{}
	send(conn, snapshot)
	waitDecision("alice", rbac.DecisionDeny)
	send(conn, authorizationSnapshot(conn.id, 5))
	waitDecision("alice", rbac.DecisionAllow)
	require.NoError(t, conn.ws.CloseNow())
	waitDecision("alice", rbac.DecisionDeny)
	conn = nextConnection()
	require.Equal(t, rbac.DecisionDeny, decision("alice"))
	snapshot = authorizationSnapshot(conn.id, 1)
	snapshot.Memberships = map[string][]string{"bob": {"readers"}}
	send(conn, snapshot)
	waitDecision("bob", rbac.DecisionAllow)
	require.Equal(t, rbac.DecisionDeny, decision("alice"))
}
