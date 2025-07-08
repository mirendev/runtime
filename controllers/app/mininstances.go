package app

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// MinInstancesController ensures that apps with min_instances configured
// have the required number of sandboxes running at all times.
type MinInstancesController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
	AA  activator.AppActivator
}

var _ controller.GenericController[*core_v1alpha.App] = (*MinInstancesController)(nil)
var _ controller.UpdatingController[*core_v1alpha.App] = (*MinInstancesController)(nil)

func (c *MinInstancesController) Init(ctx context.Context) error {
	c.Log = c.Log.With("controller", "min-instances")
	return nil
}

func (c *MinInstancesController) Create(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	return c.ensureMinInstances(ctx, app)
}

func (c *MinInstancesController) Update(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	return c.ensureMinInstances(ctx, app)
}

func (c *MinInstancesController) Delete(ctx context.Context, id entity.Id) error {
	// Nothing to do on delete - sandboxes will be cleaned up by their own lifecycle
	return nil
}

func (c *MinInstancesController) ensureMinInstances(ctx context.Context, app *core_v1alpha.App) error {
	if app.ActiveVersion == "" {
		c.Log.Debug("app has no active version", "app", app.ID)
		return nil
	}

	// Get the app version details
	vr, err := c.EAC.Get(ctx, app.ActiveVersion.String())
	if err != nil {
		return fmt.Errorf("failed to get app version: %w", err)
	}

	var appVersion core_v1alpha.AppVersion
	appVersion.Decode(vr.Entity().Entity())

	minInstances := appVersion.Config.Concurrency.MinInstances
	if minInstances <= 0 {
		c.Log.Debug("app has no min_instances configured",
			"app", app.ID,
			"version", appVersion.Version)
		return nil
	}

	c.Log.Info("ensuring min instances for app",
		"app", app.ID,
		"version", appVersion.Version,
		"min_instances", minInstances)

	// The activator's AcquireLease will create sandboxes up to min_instances
	// We just need to trigger it for each required instance
	for i := int64(0); i < minInstances; i++ {
		lease, err := c.AA.AcquireLease(ctx, &appVersion, "default", "")
		if err != nil {
			// Log but don't fail - the activator's background task will retry
			c.Log.Error("failed to ensure min instance",
				"app", app.ID,
				"version", appVersion.Version,
				"instance", i+1,
				"error", err)
			continue
		}

		// Immediately release the lease - we just needed to ensure the sandbox exists
		if lease != nil {
			if err := c.AA.ReleaseLease(ctx, lease); err != nil {
				c.Log.Warn("failed to release bootstrap lease",
					"app", app.ID,
					"error", err)
			}
		}
	}

	return nil
}
