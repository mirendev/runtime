package uplink

import (
	"encoding/json"
	"testing"
	"time"
)

func TestValidateWelcomeBuildsNegotiatedSession(t *testing.T) {
	t1 := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	hello := SessionHello{
		HandshakeVersions: []uint{HandshakeVersion1},
		RuntimeVersion:    "v1.2.3",
		ClientTime:        t1,
		Capabilities: []CapabilityOffer{{
			Name:     CapabilityPopConnect,
			Versions: []uint{1},
		}},
	}
	welcome := SessionWelcome{
		HandshakeVersion:   HandshakeVersion1,
		SessionID:          "session-1",
		OrganizationID:     "org-1",
		ServerReceiveTime:  t1.Add(12 * time.Millisecond),
		ServerTransmitTime: t1.Add(14 * time.Millisecond),
		Capabilities: []CapabilitySelection{{
			Name:    CapabilityPopConnect,
			Version: 1,
			Config:  json.RawMessage(`{"max_connections":8}`),
		}},
		IdentityIssuerURL: "https://id.example.test/cluster-1",
	}

	session, err := validateWelcome(hello, welcome, t1.Add(22*time.Millisecond))
	if err != nil {
		t.Fatalf("validate welcome: %v", err)
	}
	if session.ID != "session-1" || session.OrganizationID != "org-1" {
		t.Fatalf("unexpected session identity: %+v", session)
	}
	if session.ClockOffset != 2*time.Millisecond {
		t.Fatalf("clock offset = %v, want 2ms", session.ClockOffset)
	}
	capability, ok := session.Capability(CapabilityPopConnect)
	if !ok || capability.Version != 1 {
		t.Fatalf("pop-connect selection = %+v, %v", capability, ok)
	}
	if session.IdentityIssuerURL != "https://id.example.test/cluster-1" {
		t.Fatalf("identity anchor = %q, want the welcome's", session.IdentityIssuerURL)
	}
}

// A cloud that predates the anchor, or is not serving discovery, sends none;
// the session simply carries an empty one rather than failing.
func TestValidateWelcomeWithoutIdentityAnchor(t *testing.T) {
	t1 := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	hello := SessionHello{HandshakeVersions: []uint{HandshakeVersion1}, RuntimeVersion: "v1", ClientTime: t1}
	welcome := SessionWelcome{
		HandshakeVersion: HandshakeVersion1, SessionID: "session-1", OrganizationID: "org-1",
		ServerReceiveTime: t1, ServerTransmitTime: t1,
	}
	session, err := validateWelcome(hello, welcome, t1)
	if err != nil {
		t.Fatalf("validate welcome: %v", err)
	}
	if session.IdentityIssuerURL != "" {
		t.Fatalf("identity anchor = %q, want empty", session.IdentityIssuerURL)
	}
}

func TestValidateWelcomeRejectsInvalidSelections(t *testing.T) {
	now := time.Now().UTC()
	hello := SessionHello{
		HandshakeVersions: []uint{1},
		RuntimeVersion:    "test",
		ClientTime:        now,
		Capabilities: []CapabilityOffer{{
			Name:     CapabilityPopConnect,
			Versions: []uint{1},
		}},
	}
	valid := SessionWelcome{
		HandshakeVersion:   1,
		SessionID:          "session-1",
		OrganizationID:     "org-1",
		ServerReceiveTime:  now,
		ServerTransmitTime: now,
	}

	tests := []struct {
		name   string
		mutate func(*SessionWelcome)
	}{
		{"unoffered handshake", func(w *SessionWelcome) { w.HandshakeVersion = 2 }},
		{"missing session id", func(w *SessionWelcome) { w.SessionID = "" }},
		{"missing organization", func(w *SessionWelcome) { w.OrganizationID = "" }},
		{"unoffered capability", func(w *SessionWelcome) {
			w.Capabilities = []CapabilitySelection{{Name: "entity-sync", Version: 1}}
		}},
		{"unoffered capability version", func(w *SessionWelcome) {
			w.Capabilities = []CapabilitySelection{{Name: CapabilityPopConnect, Version: 2}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			welcome := valid
			tt.mutate(&welcome)
			if _, err := validateWelcome(hello, welcome, now); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
