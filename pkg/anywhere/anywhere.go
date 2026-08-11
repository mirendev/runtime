// Package anywhere implements the runtime-side connector for Miren Anywhere,
// the connectivity feature that links a cluster to Miren Cloud's POP network
// for NAT traversal and inbound request forwarding.
//
// The name is outgrowing the contents. This package holds two different
// things, and RFD-94 separates them explicitly:
//
//   - The *control plane* — Client, MessageRouter, Envelope and the connect
//     hooks. One persistent, payload-agnostic link per cluster, which the RFD
//     calls the unified uplink and treats as the backbone for all runtime↔cloud
//     coordination. App reporting is simply its first heavyweight tenant.
//   - The *data plane* — POPManager and connection.request. Many connections,
//     Anywhere-specific, existing to forward public traffic into a NAT'd
//     cluster.
//
// They couple at exactly one seam: the data plane's setup handshake is
// delivered over the control plane. So the control-plane half wants to live in
// its own package with Anywhere as one tenant among several, rather than every
// future tenant importing a package named for NAT traversal. Renaming this
// package wholesale would not fix it, since POPManager genuinely is Anywhere.
// Tracked in MIR-1568.
package anywhere

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/servers/httpingress"
)

// Config holds the configuration for the Miren Anywhere connector.
type Config struct {
	CloudURL   string
	ClusterXID string
	AuthClient *cloudauth.AuthClient
	Ingress    *httpingress.Server
	Log        *slog.Logger
}

// Connector connects the cluster to the cloud coordination service
// and manages POP connections for inbound traffic forwarding. It is the
// runtime side of Miren Anywhere.
type Connector struct {
	client *Client
	router *MessageRouter
	pops   *POPManager
	log    *slog.Logger

	mu             sync.RWMutex
	timeOffset     time.Duration
	organizationID string
}

// New creates a Connector. It wires up the cloud client and POP manager,
// registers the connection, time-sync, and org-info message handlers, and
// arranges for a time-sync and org-info request to be sent on each new
// connection.
func New(cfg Config) *Connector {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	router := NewMessageRouter()

	client := NewClient(cfg.CloudURL, cfg.AuthClient, router, log)
	pops := NewPOPManager(cfg.ClusterXID, cfg.Ingress, log)

	c := &Connector{
		client: client,
		router: router,
		pops:   pops,
		log:    log,
	}

	// Register message handlers
	router.Handle(TypeConnectionRequest, c.handleConnectionRequest)
	router.Handle(TypeTimeResponse, c.handleTimeResponse)
	router.Handle(TypeOrgInfoResponse, c.handleOrgInfoResponse)

	// On each new connection, request time sync and org info
	client.OnConnect(func(ctx context.Context) {
		c.log.Info("sending initial requests")
		client.SendMessage(TypeTimeRequest, TimeRequest{
			ClientTransmitTime: time.Now().UTC(),
		})
		client.SendMessage(TypeOrgInfoRequest, struct{}{})
	})

	return c
}

// Handle registers a handler for an inbound message type, letting features
// outside this package become tenants of the uplink. Message types are
// namespaced by feature (app.*, deploy.*) so the channel stays a shared
// pipe rather than accumulating per-feature special cases.
func (c *Connector) Handle(msgType string, handler MessageHandler) {
	c.router.Handle(msgType, handler)
}

// OnConnect registers a callback invoked on each (re)connection, after the
// connector's own time-sync and org-info requests are queued. Callbacks
// accumulate, so registering one does not disturb the connector's.
//
// This is the hook state reporting hangs off: the uplink drops whatever was
// queued while disconnected, so anything produced during an outage has to be
// re-derived from the source on reconnect rather than replayed from a buffer.
func (c *Connector) OnConnect(fn func(ctx context.Context)) {
	c.client.OnConnect(fn)
}

// SendMessage marshals data and queues it for delivery to cloud.
//
// Queueing is best-effort: the outbox is bounded and drops on overflow, and
// a disconnect discards whatever is still queued. Callers must treat delivery
// as unreliable and reconcile from their own durable source instead of
// assuming a send arrived.
func (c *Connector) SendMessage(msgType string, data any) error {
	return c.client.SendMessage(msgType, data)
}

// Run starts the connector. It blocks until the context is cancelled.
func (c *Connector) Run(ctx context.Context) error {
	c.log.Info("Miren Anywhere connector starting")
	defer c.log.Info("Miren Anywhere connector stopped")
	defer c.pops.Close()
	return c.client.Run(ctx)
}

// TimeOffset returns the estimated clock offset between this cluster
// and the cloud, computed via simplified NTP.
func (c *Connector) TimeOffset() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.timeOffset
}

// OrganizationID returns the organization ID reported by the cloud.
func (c *Connector) OrganizationID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.organizationID
}

func (c *Connector) handleConnectionRequest(ctx context.Context, data json.RawMessage) error {
	var req ConnectionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	return c.pops.HandleConnectionRequest(ctx, req)
}

func (c *Connector) handleTimeResponse(ctx context.Context, data json.RawMessage) error {
	t4 := time.Now().UTC()

	var resp TimeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}

	// Simplified NTP offset calculation:
	// offset = ((T2 - T1) + (T3 - T4)) / 2
	t1 := resp.ClientTransmitTime
	t2 := resp.ServerReceiveTime
	t3 := resp.ServerTransmitTime

	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2

	c.mu.Lock()
	c.timeOffset = offset
	c.mu.Unlock()

	c.log.Info("clock sync complete", "offset", offset)
	return nil
}

func (c *Connector) handleOrgInfoResponse(ctx context.Context, data json.RawMessage) error {
	var resp OrgInfoResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}

	c.mu.Lock()
	c.organizationID = resp.OrganizationID
	c.mu.Unlock()

	c.log.Info("organization info received", "org_id", resp.OrganizationID)
	return nil
}
