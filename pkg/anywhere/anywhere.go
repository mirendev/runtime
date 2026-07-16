// Package anywhere implements the runtime-side connector for Miren Anywhere,
// the connectivity feature that links a cluster to Miren Cloud's POP network
// for NAT traversal and inbound request forwarding.
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
