package anywhere

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Message type constants matching the cloud's wire protocol.
const (
	TypeTimeRequest     = "time.request"
	TypeTimeResponse    = "time.response"
	TypeOrgInfoRequest  = "org.info.request"
	TypeOrgInfoResponse = "org.info.response"

	// TypeConnectionRequest is sent from cloud to cluster when a POP
	// server needs the cluster to establish an HTTP/3 connection.
	TypeConnectionRequest = "connection.request"

	// TypeAppSnapshot carries the cluster's full current app set to cloud
	// for visibility reporting. Part of the app.* family described in
	// RFD-94; see AppSnapshot for the contract.
	TypeAppSnapshot = "app.snapshot"
)

// AppSnapshot is the cluster's view of every app it currently runs, sent
// on each (re)connect. Cloud treats it as ground truth for that cluster.
//
// This is the ephemeral tier of RFD-94's volatility spectrum: the weakest
// durability contract in the design. Delivery is best-effort because loss
// self-heals — a dropped snapshot is repaired by the next one rather than
// retried, so nothing here is worth an ack.
//
// Two things are deliberately absent and belong to the follow-up that adds
// sync correctness:
//
//   - No epoch. Cloud upserts what it receives and never sweeps, so an app
//     deleted in the cluster lingers in cloud until epochs arrive and make
//     mark-and-sweep safe. Sweeping without an epoch would soft-delete apps
//     that simply haven't landed yet, since a real snapshot arrives in
//     batches rather than atomically.
//   - No instance counts or resource usage. Health alone proves the seam.
type AppSnapshot struct {
	// ObservedAt is the runtime's own clock reading for when this state was
	// true. Cloud reconciles it against its own clock using the offset from
	// the existing time-sync exchange, and applies last-writer-wins with it,
	// so a delayed message cannot clobber fresher state.
	ObservedAt time.Time `json:"observed_at"`

	Apps []AppState `json:"apps"`
}

// AppState is one app's current sample within a snapshot.
//
// Note there is no cluster or organization field. Cloud derives both from
// the authenticated cluster identity on the socket and its own clusters
// record — a cluster is never trusted to say which tenant its data belongs
// to, only what its own apps are doing.
type AppState struct {
	Name string `json:"name"`

	// Health is the runtime's classification, one of the apphealth values:
	// healthy, degraded, starting, crashed, idle, or unknown. It is computed
	// the same way `miren app list` computes it, deliberately, so the CLI and
	// the dashboard cannot disagree about whether an app is fine.
	Health string `json:"health"`
}

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

// ConnectionRequest is the payload for connection.request messages.
type ConnectionRequest struct {
	POPXID       string `json:"pop_xid"`
	POPAddress   string `json:"pop_address"`
	Hostname     string `json:"hostname"`
	RequestID    string `json:"request_id"`
	ConnectToken string `json:"connect_token"`
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
