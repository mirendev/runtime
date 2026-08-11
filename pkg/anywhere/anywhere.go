// Package anywhere implements the runtime side of Miren Anywhere, the
// connectivity feature that links a cluster to Miren Cloud's POP network for
// NAT traversal and inbound request forwarding.
//
// This is the data plane: many HTTP/3-over-QUIC connections, one per (cluster,
// POP), optimized for bandwidth and latency and carrying public traffic into a
// NAT'd cluster. It is a tenant of the control-plane link in pkg/uplink, which
// it uses for exactly one thing: receiving the connection.request handshake
// that tells it to dial a POP.
package anywhere

import (
	"context"
	"encoding/json"
	"log/slog"

	"miren.dev/runtime/pkg/uplink"
	"miren.dev/runtime/servers/httpingress"
)

// Config holds the configuration for the Miren Anywhere connector.
type Config struct {
	ClusterXID string
	Ingress    *httpingress.Server
	Log        *slog.Logger

	// Uplink is the control-plane link this connector rides on. The connector
	// registers its handler against it but does not own its lifecycle; whoever
	// created the link runs it.
	Uplink *uplink.Client
}

// Connector manages POP connections for inbound traffic forwarding.
type Connector struct {
	pops *POPManager
	log  *slog.Logger
}

// New creates a Connector and registers its connection.request handler on the
// uplink. The caller is responsible for running the uplink itself.
func New(cfg Config) *Connector {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	c := &Connector{
		pops: NewPOPManager(cfg.ClusterXID, cfg.Ingress, log),
		log:  log,
	}

	cfg.Uplink.Handle(TypeConnectionRequest, c.handleConnectionRequest)

	return c
}

// Close tears down POP connections.
func (c *Connector) Close() {
	c.pops.Close()
}

func (c *Connector) handleConnectionRequest(ctx context.Context, data json.RawMessage) error {
	var req ConnectionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	return c.pops.HandleConnectionRequest(ctx, req)
}
