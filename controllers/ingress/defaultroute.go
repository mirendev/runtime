package ingress

import (
	"context"
	"log/slog"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// DefaultRouteController ensures only one http_route can be marked as default at a time.
type DefaultRouteController struct {
	Log *slog.Logger
	ic  *ingress.Client
}

// NewDefaultRouteController creates a new DefaultRouteController
func NewDefaultRouteController(log *slog.Logger, rc rpc.Client) *DefaultRouteController {
	return &DefaultRouteController{
		Log: log.With("module", "default-route-controller"),
		ic:  ingress.NewClient(log, rc),
	}
}

// Meets GenericController interface requirements
func (c *DefaultRouteController) Init(context.Context) error {
	return nil
}

// Create handles http_route creation/update events
func (c *DefaultRouteController) Create(ctx context.Context, route *ingress_v1alpha.HttpRoute, meta *entity.Meta) error {
	c.Log.Info("HttpRoute created", "route", route.ID, "default", route.Default)

	// If this route is marked as default, ensure no other routes are default
	if route.Default {
		return c.ic.EnsureSingleDefault(ctx, route)
	}

	return nil
}

// Update handles http_route update events
func (c *DefaultRouteController) Update(ctx context.Context, route *ingress_v1alpha.HttpRoute, meta *entity.Meta) error {
	c.Log.Info("HttpRoute updated", "route", route.ID, "default", route.Default)

	// If this route is being updated with default=true, ensure no other routes are default
	if route.Default {
		c.Log.Info("HttpRoute updated to default=true, enforcing single-default invariant", "route", route.ID)
		return c.ic.EnsureSingleDefault(ctx, route)
	}

	// If route.Default is set to false, we explicitly do NOT automatically promote
	// another route to default. This maintains explicit control over the default
	// designation and avoids unexpected state changes.
	c.Log.Debug("HttpRoute updated w/ default=false, no automatic promotion of other routes", "route", route.ID)
	return nil
}

// Delete handles http_route deletion events
func (c *DefaultRouteController) Delete(ctx context.Context, id entity.Id) error {
	c.Log.Info("HttpRoute deleted; doing nothing", "route", id)
	// Note: We don't automatically promote another route to default when
	// the default route is deleted, as per the requirements
	return nil
}
