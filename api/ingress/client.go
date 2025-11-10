package ingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// Client provides a domain-specific client for HttpRoute entities
type Client struct {
	log *slog.Logger
	ec  *entityserver.Client
	eac *entityserver_v1alpha.EntityAccessClient
}

// NewClient creates a new Ingress client from an RPC client
func NewClient(log *slog.Logger, client rpc.Client) *Client {
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	entityClient := entityserver.NewClient(log, eac)

	return &Client{
		log: log.With("module", "ingress-client"),
		ec:  entityClient,
		eac: eac,
	}
}

// Lookup finds an http_route by hostname, returns nil if not found
func (c *Client) Lookup(ctx context.Context, host string) (*ingress_v1alpha.HttpRoute, error) {
	ia := entity.String(ingress_v1alpha.HttpRouteHostId, host)

	var route ingress_v1alpha.HttpRoute
	err := c.ec.OneAtIndex(ctx, ia, &route)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("failed to lookup route for host %s: %w", host, err)
		}
	}

	return &route, nil
}

// LookupDefault finds the default http_route
func (c *Client) LookupDefault(ctx context.Context) (*ingress_v1alpha.HttpRoute, error) {
	var route ingress_v1alpha.HttpRoute
	err := c.ec.OneAtIndex(ctx, entity.Bool(ingress_v1alpha.HttpRouteDefaultId, true), &route)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("failed to lookup default route: %w", err)
		}
	}
	return &route, nil
}

// SetDefault sets the default route to the provided app
func (c *Client) SetDefault(ctx context.Context, appId entity.Id) (*ingress_v1alpha.HttpRoute, error) {
	// Since host is blank for default routes, and it's normally used for the ID field, we make a special ID format
	routeId := fmt.Sprintf("default-%s", appId)

	route := &ingress_v1alpha.HttpRoute{
		ID:      entity.Id(routeId),
		App:     appId,
		Default: true,
	}
	if _, err := c.ec.CreateOrUpdate(ctx, routeId, route); err != nil {
		return nil, fmt.Errorf("failed to create default route: %w", err)
	}

	return route, nil
}

// UnsetDefault unsets the default route, if any. It returns the route that it unset the default from.
func (c *Client) UnsetDefault(ctx context.Context) (*ingress_v1alpha.HttpRoute, error) {
	route, err := c.LookupDefault(ctx)
	if err != nil {
		return nil, err
	}

	if route == nil {
		return nil, nil
	}

	if err := c.ec.Delete(ctx, route.ID); err != nil {
		return nil, fmt.Errorf("failed to delete default route: %w", err)
	}

	return route, nil
}

// EnsureSingleDefault removes any default routes but the one specified
func (c *Client) EnsureSingleDefault(ctx context.Context, routeToKeep *ingress_v1alpha.HttpRoute) error {
	resp, err := c.ec.List(ctx, entity.Bool(ingress_v1alpha.HttpRouteDefaultId, true))
	if err != nil {
		return fmt.Errorf("failed to list default routes: %w", err)
	}

	for resp.Next() {
		var route ingress_v1alpha.HttpRoute
		if err := resp.Read(&route); err != nil {
			c.log.Error("Failed to read route", "error", err)
			continue
		}

		// Skip the route we want to keep as default
		if route.ID == routeToKeep.ID {
			continue
		}

		c.log.Info("Deleting old default route", "route", route.ID)
		if err := c.ec.Delete(ctx, route.ID); err != nil {
			return fmt.Errorf("failed to delete old default route %s: %w", route.ID, err)
		}
	}

	return nil
}

// RouteWithMeta includes an http_route with its metadata
type RouteWithMeta struct {
	Route     *ingress_v1alpha.HttpRoute
	CreatedAt int64
	UpdatedAt int64
}

// List returns all http_routes with metadata
func (c *Client) List(ctx context.Context) ([]*RouteWithMeta, error) {
	kindRes, err := c.eac.LookupKind(ctx, "http_route")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup http_route kind: %w", err)
	}

	res, err := c.eac.List(ctx, kindRes.Attr())
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	var routes []*RouteWithMeta
	for _, e := range res.Values() {
		var route ingress_v1alpha.HttpRoute
		route.Decode(e.Entity())
		routes = append(routes, &RouteWithMeta{
			Route:     &route,
			CreatedAt: e.CreatedAt(),
			UpdatedAt: e.UpdatedAt(),
		})
	}

	return routes, nil
}

// SetRoute creates or updates an http_route for the given host and app
func (c *Client) SetRoute(ctx context.Context, host string, appId entity.Id) (*ingress_v1alpha.HttpRoute, error) {
	route := &ingress_v1alpha.HttpRoute{
		Host: host,
		App:  appId,
	}

	// Use the host as the route name/ID
	_, err := c.ec.CreateOrUpdate(ctx, host, route)
	if err != nil {
		return nil, fmt.Errorf("failed to create/update route: %w", err)
	}

	return route, nil
}

// DeleteByHost deletes an http_route by hostname
func (c *Client) DeleteByHost(ctx context.Context, host string) error {
	route, err := c.Lookup(ctx, host)
	if err != nil {
		return err
	}

	if route == nil {
		return fmt.Errorf("route not found: %s", host)
	}

	if err := c.ec.Delete(ctx, route.ID); err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}

	return nil
}
