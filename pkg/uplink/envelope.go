// Package uplink implements the cluster's control-plane link to Miren Cloud:
// exactly one persistent, authenticated WebSocket per cluster, carrying typed
// coordination messages multiplexed by type.
//
// RFD-94 calls this the unified uplink and treats it as the backbone for all
// runtime↔cloud coordination. Features are tenants of it — Miren Anywhere for
// NAT traversal, app state reporting, and whatever follows — and register their
// own handlers and connect hooks rather than the link knowing anything about
// them.
//
// The pipe is payload-agnostic on purpose, and should stay that way. It moves
// opaque Envelopes and understands no apps, deploys, or POPs. That is separate
// from it being delivery-unreliable, which it also is: the outbox is bounded and
// drops on overflow, and a reconnect discards whatever was queued. Tenants that
// need completeness reconcile from their own durable source rather than
// expecting the wire to have carried everything.
//
// The exception to "knows nothing" is deliberate and small: clock offset and
// organization identity are established here, because both are properties of
// the link itself rather than of any one tenant.
package uplink

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Control-plane message types. Feature message types are namespaced by their
// own family (app.*, deploy.*) and defined by the tenant that owns them, not
// here.
const (
	TypeTimeRequest     = "time.request"
	TypeTimeResponse    = "time.response"
	TypeOrgInfoRequest  = "org.info.request"
	TypeOrgInfoResponse = "org.info.response"
)

// Envelope is the wire format for all messages on the WebSocket.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// MessageHandler processes an inbound message.
type MessageHandler func(ctx context.Context, data json.RawMessage) error

// MessageRouter dispatches inbound messages by type.
type MessageRouter struct {
	mu       sync.RWMutex
	handlers map[string]MessageHandler
}

// NewMessageRouter creates a new MessageRouter.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make(map[string]MessageHandler),
	}
}

// Handle registers a handler for a given message type.
func (r *MessageRouter) Handle(msgType string, handler MessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[msgType] = handler
}

// Dispatch routes a message to the appropriate handler.
func (r *MessageRouter) Dispatch(ctx context.Context, env Envelope) error {
	r.mu.RLock()
	handler, ok := r.handlers[env.Type]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler registered for message type %q", env.Type)
	}

	return handler(ctx, env.Data)
}

// TimeRequest is the payload for time.request messages.
type TimeRequest struct {
	ClientTransmitTime time.Time `json:"client_transmit_time"`
}

// TimeResponse carries the four timestamps needed for simplified NTP.
type TimeResponse struct {
	ClientTransmitTime time.Time `json:"client_transmit_time"`
	ServerReceiveTime  time.Time `json:"server_receive_time"`
	ServerTransmitTime time.Time `json:"server_transmit_time"`
}

// OrgInfoResponse is the payload for org.info.response messages.
type OrgInfoResponse struct {
	OrganizationID string `json:"organization_id"`
}
