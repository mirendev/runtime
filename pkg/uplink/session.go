package uplink

// This file is the runtime half of the bootstrap wire contract. Its cloud
// counterpart is mirendev/cloud/services/cluster_channel/session.go. Keep the
// JSON shapes in lockstep until the uplink has a shared schema home.

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const (
	TypeSessionHello   = "session.hello"
	TypeSessionWelcome = "session.welcome"
	TypeSessionReject  = "session.reject"

	HandshakeVersion1 uint = 1

	CapabilityPopConnect = "pop-connect"
	CapabilityRPCRelay   = "rpc-relay"
	CapabilityEntitySync = "entity-sync"
	CapabilityAppHealth  = "app-health"
)

// CapabilityOffer describes one protocol family the runtime can speak. The
// session layer negotiates the version but leaves Offer to the capability. For
// example, entity sync will use it to offer export-schema digests without
// teaching the uplink what an entity is.
type CapabilityOffer struct {
	Name     string          `json:"name"`
	Versions []uint          `json:"versions"`
	Offer    json.RawMessage `json:"offer,omitempty"`
}

// CapabilitySelection is cloud's choice for one offered capability. Config is
// owned and decoded by that capability, not by the session layer.
type CapabilitySelection struct {
	Name    string          `json:"name"`
	Version uint            `json:"version"`
	Config  json.RawMessage `json:"config,omitempty"`
}

// SessionHello is the first envelope sent on a negotiated uplink connection.
// The bootstrap shape is deliberately small: future protocol families evolve
// behind capabilities rather than adding their state directly here.
type SessionHello struct {
	HandshakeVersions []uint            `json:"handshake_versions"`
	RuntimeVersion    string            `json:"runtime_version"`
	ClientTime        time.Time         `json:"client_time"`
	Capabilities      []CapabilityOffer `json:"capabilities"`
}

// SessionWelcome establishes the session and selects the protocol families
// both peers will use for this connection.
type SessionWelcome struct {
	HandshakeVersion   uint                  `json:"handshake_version"`
	SessionID          string                `json:"session_id"`
	OrganizationID     string                `json:"organization_id"`
	ServerReceiveTime  time.Time             `json:"server_receive_time"`
	ServerTransmitTime time.Time             `json:"server_transmit_time"`
	Capabilities       []CapabilitySelection `json:"capabilities"`
}

// SessionReject explains why cloud could not establish a negotiated session.
// It is a bootstrap message, so it must remain decodable by every handshake
// version.
type SessionReject struct {
	Reason                     string `json:"reason"`
	SupportedHandshakeVersions []uint `json:"supported_handshake_versions,omitempty"`
}

// Session is immutable negotiated state scoped to one WebSocket connection.
type Session struct {
	ID               string
	HandshakeVersion uint
	RuntimeVersion   string
	OrganizationID   string
	ClockOffset      time.Duration
	Capabilities     []CapabilitySelection
}

// Capability returns the selected capability with the given name.
func (s Session) Capability(name string) (CapabilitySelection, bool) {
	for _, capability := range s.Capabilities {
		if capability.Name == name {
			return capability, true
		}
	}
	return CapabilitySelection{}, false
}

func validateWelcome(hello SessionHello, welcome SessionWelcome, receivedAt time.Time) (Session, error) {
	if welcome.SessionID == "" {
		return Session{}, fmt.Errorf("session welcome has no session id")
	}
	if welcome.OrganizationID == "" {
		return Session{}, fmt.Errorf("session welcome has no organization")
	}
	if !slices.Contains(hello.HandshakeVersions, welcome.HandshakeVersion) {
		return Session{}, fmt.Errorf("cloud selected unoffered handshake version %d", welcome.HandshakeVersion)
	}
	if hello.ClientTime.IsZero() || welcome.ServerReceiveTime.IsZero() || welcome.ServerTransmitTime.IsZero() {
		return Session{}, fmt.Errorf("session welcome has incomplete clock timestamps")
	}

	offered := make(map[string]CapabilityOffer, len(hello.Capabilities))
	for _, capability := range hello.Capabilities {
		if capability.Name == "" {
			return Session{}, fmt.Errorf("session hello contains unnamed capability")
		}
		if _, exists := offered[capability.Name]; exists {
			return Session{}, fmt.Errorf("session hello contains duplicate capability %q", capability.Name)
		}
		offered[capability.Name] = capability
	}

	selected := make(map[string]struct{}, len(welcome.Capabilities))
	for _, capability := range welcome.Capabilities {
		offer, ok := offered[capability.Name]
		if !ok {
			return Session{}, fmt.Errorf("cloud selected unoffered capability %q", capability.Name)
		}
		if _, exists := selected[capability.Name]; exists {
			return Session{}, fmt.Errorf("cloud selected capability %q more than once", capability.Name)
		}
		if !slices.Contains(offer.Versions, capability.Version) {
			return Session{}, fmt.Errorf("cloud selected unoffered %s version %d", capability.Name, capability.Version)
		}
		selected[capability.Name] = struct{}{}
	}

	offset := (welcome.ServerReceiveTime.Sub(hello.ClientTime) + welcome.ServerTransmitTime.Sub(receivedAt)) / 2
	return Session{
		ID:               welcome.SessionID,
		HandshakeVersion: welcome.HandshakeVersion,
		RuntimeVersion:   hello.RuntimeVersion,
		OrganizationID:   welcome.OrganizationID,
		ClockOffset:      offset,
		Capabilities:     slices.Clone(welcome.Capabilities),
	}, nil
}
