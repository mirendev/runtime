package app

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// DefaultAppController manages the default app designation.
// It ensures only one app can be marked as default at a time,
// and automatically marks the first app as default if none exists.
type DefaultAppController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
}

// NewDefaultAppController creates a new DefaultAppController
func NewDefaultAppController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *DefaultAppController {
	return &DefaultAppController{
		Log: log.With("module", "default-app-controller"),
		EAC: eac,
	}
}

// Init initializes the controller
func (c *DefaultAppController) Init(ctx context.Context) error {
	c.Log.Info("Initializing DefaultAppController")
	return nil
}

// Create handles app creation/update events
func (c *DefaultAppController) Create(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	c.Log.Info("App created", "app", app.ID)

	// If this app is already marked as default, ensure no other apps are default
	if app.Default {
		return c.ensureSingleDefault(ctx, app.ID)
	}

	// If this app is not marked as default, check if we need to make it default
	// (i.e., if no other app is currently default)
	hasDefault, err := c.hasDefaultApp(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for existing default app: %w", err)
	}

	// If no app is currently default, make this one default
	if !hasDefault {
		c.Log.Info("No default app found, marking as default", "app", app.ID)
		app.Default = true

		// Update the entity in meta so the change gets persisted
		updatedAttrs := app.Encode()
		meta.Entity.Attrs = updatedAttrs
	}

	return nil
}

// Update handles app update events
func (c *DefaultAppController) Update(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	c.Log.Info("App updated", "app", app.ID, "default", app.Default)

	// If this app is being updated with default=true, ensure no other apps are default
	if app.Default {
		c.Log.Info("App updated to default=true, enforcing single-default invariant", "app", app.ID)
		return c.ensureSingleDefault(ctx, app.ID)
	}

	// If app.Default is set to false, we explicitly do NOT automatically promote
	// another app to default. This maintains explicit control over the default
	// designation and avoids unexpected state changes. Also note we don't yet
	// have a way of detecting at this layer whether or not a change to this attr
	// was explicitly made
	c.Log.Debug("App updated w/ default=false, no automatic promotion of other apps", "app", app.ID)
	return nil
}

// Delete handles app deletion events
func (c *DefaultAppController) Delete(ctx context.Context, id entity.Id) error {
	c.Log.Info("App deleted; doing nothing", "app", id)
	// Note: We don't automatically promote another app to default when
	// the default app is deleted, as per the requirements
	return nil
}

// ensureSingleDefault ensures that only the specified app is marked as default
func (c *DefaultAppController) ensureSingleDefault(ctx context.Context, keepDefaultId entity.Id) error {
	// Find all apps that are currently marked as default
	resp, err := c.EAC.List(ctx, entity.Bool(core_v1alpha.AppDefaultId, true))
	if err != nil {
		return fmt.Errorf("failed to list default apps: %w", err)
	}

	for _, ent := range resp.Values() {
		var app core_v1alpha.App
		app.Decode(ent.Entity())

		// Skip the app we want to keep as default
		if app.ID == keepDefaultId {
			continue
		}

		// Remove default flag from this app
		c.Log.Info("Removing default flag from app", "app", app.ID)
		app.Default = false

		// Update the app
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(string(app.ID))
		rpcE.SetAttrs(app.Encode())

		_, err := c.EAC.Put(ctx, &rpcE)
		if err != nil {
			c.Log.Error("Failed to remove default flag from app", "app", app.ID, "error", err)
			return fmt.Errorf("failed to update app %s: %w", app.ID, err)
		}
	}

	return nil
}

// hasDefaultApp checks if any app is currently marked as default
func (c *DefaultAppController) hasDefaultApp(ctx context.Context) (bool, error) {
	resp, err := c.EAC.List(ctx, entity.Bool(core_v1alpha.AppDefaultId, true))
	if err != nil {
		return false, fmt.Errorf("failed to list default apps: %w", err)
	}

	return len(resp.Values()) > 0, nil
}
