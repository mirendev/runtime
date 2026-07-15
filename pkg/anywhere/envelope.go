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
