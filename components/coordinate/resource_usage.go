package coordinate

import (
	"context"
	"errors"

	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	usagesrv "miren.dev/runtime/servers/usage"
)

// NewResourceUsage constructs the cluster-wide usage service on top of an
// already-created foundation.
func NewResourceUsage(foundation *Foundation) *ResourceUsage {
	return &ResourceUsage{Foundation: foundation}
}

// ResourceUsage answers what the cluster is spending its CPU and memory on.
//
// This lives only on the coordinator because it is the only process holding
// both the entity store and a reader for every node's metrics; a runner can
// answer for itself but not for the cluster.
type ResourceUsage struct {
	*Foundation
}

// Start exposes usage queries to clients. A nil MetricsReader is not an error:
// the service still answers "what is running where" from the entity store, and
// reports the figures it cannot measure as warnings.
func (c *ResourceUsage) Start(ctx context.Context) error {
	if c.state == nil || c.eac == nil {
		return errors.New("cluster foundation is not ready")
	}

	ec := aes.NewClient(c.Log, c.eac)

	server := c.state.Server()
	server.ExposeValue("dev.miren.runtime/usage",
		usage_v1alpha.AdaptResourceUsage(usagesrv.NewServer(c.Log, ec, c.MetricsReader)))

	return nil
}
