package ingress

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// DefaultRouteAppController manages default routes based on app lifecycle.
// It watches for app creation/deletion and manages default http_routes:
// - When the first app is created, it creates a default route for it
// - When the last app is deleted, it removes all default routes
type DefaultRouteAppController struct {
	Log *slog.Logger
	// TODO: Swap this out for higher level app.Client
	EAC *entityserver_v1alpha.EntityAccessClient
	ic  *ingress.Client
}

// NewDefaultRouteAppController creates a new DefaultRouteAppController
func NewDefaultRouteAppController(log *slog.Logger, rc rpc.Client) *DefaultRouteAppController {
	return &DefaultRouteAppController{
		Log: log.With("module", "default-route-app-controller"),
		EAC: entityserver_v1alpha.NewEntityAccessClient(rc),
		ic:  ingress.NewClient(log, rc),
	}
}

// Meets GenericController interface requirements
func (c *DefaultRouteAppController) Init(context.Context) error {
	return nil
}

// Create handles app creation events
func (c *DefaultRouteAppController) Create(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	c.Log.Info("App created", "app", app.ID)

	if !app.DeletedAt.IsZero() {
		c.Log.Debug("App is deleted, skipping", "app", app.ID)
		return nil
	}

	appList, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	activeCount := 0
	for _, e := range appList.Values() {
		var a core_v1alpha.App
		a.Decode(e.Entity())
		if a.DeletedAt.IsZero() {
			activeCount++
		}
	}

	if activeCount == 1 {
		c.Log.Info("First app created, creating default route", "app", app.ID)

		route, err := c.ic.SetDefault(ctx, app.ID)
		if err != nil {
			return fmt.Errorf("failed to create default route: %w", err)
		}

		c.Log.Info("Created default route", "route", route.ID, "app", app.ID)
	}

	return nil
}

// Update handles app update events - check if app was soft-deleted
func (c *DefaultRouteAppController) Update(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	c.Log.Debug("App updated", "app", app.ID, "deleted", !app.DeletedAt.IsZero())

	if !app.DeletedAt.IsZero() {
		defaultRoute, err := c.ic.LookupDefault(ctx)
		if err != nil {
			return fmt.Errorf("failed to lookup default route: %w", err)
		}

		if defaultRoute != nil && defaultRoute.App == app.ID {
			c.Log.Info("Soft-deleted app had default route, removing it", "app", app.ID, "route", defaultRoute.ID)
			if _, err := c.ic.UnsetDefault(ctx); err != nil {
				return fmt.Errorf("failed to unset default route: %w", err)
			}
		}

		appList, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
		if err != nil {
			return fmt.Errorf("failed to list apps: %w", err)
		}

		activeCount := 0
		for _, e := range appList.Values() {
			var a core_v1alpha.App
			a.Decode(e.Entity())
			if a.DeletedAt.IsZero() {
				activeCount++
			}
		}

		if activeCount == 0 {
			c.Log.Info("Last app soft-deleted, removing any remaining default routes")
			_, err := c.ic.UnsetDefault(ctx)
			return err
		}
	}

	return nil
}

// Delete handles app deletion events
func (c *DefaultRouteAppController) Delete(ctx context.Context, id entity.Id) error {
	c.Log.Info("App deleted", "app", id)

	// Check if this app had a default route
	defaultRoute, err := c.ic.LookupDefault(ctx)
	if err != nil {
		return fmt.Errorf("failed to lookup default route: %w", err)
	}

	// If this app had the default route, delete it
	if defaultRoute != nil && defaultRoute.App == id {
		c.Log.Info("Deleted app had default route, removing it", "app", id, "route", defaultRoute.ID)
		if _, err := c.ic.UnsetDefault(ctx); err != nil {
			return fmt.Errorf("failed to unset default route: %w", err)
		}
	}

	appList, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	activeCount := 0
	for _, e := range appList.Values() {
		var a core_v1alpha.App
		a.Decode(e.Entity())
		if a.DeletedAt.IsZero() {
			activeCount++
		}
	}

	if activeCount == 0 {
		c.Log.Info("Last app deleted, removing any remaining default routes")
		_, err := c.ic.UnsetDefault(ctx)
		return err
	}

	return nil
}
