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

// DeploymentController manages app version deployments.
// It watches for app creation/updates and coordinates with the activator to:
// - Deploy new app versions
// - Ensure min_instances are running
// - Clean up old app versions and their sandboxes
type DeploymentController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
	AA  activator.AppActivator
}

var _ controller.GenericController[*core_v1alpha.App] = (*DeploymentController)(nil)
var _ controller.UpdatingController[*core_v1alpha.App] = (*DeploymentController)(nil)

func (c *DeploymentController) Init(ctx context.Context) error {
	c.Log = c.Log.With("controller", "deployment")
	return nil
}

func (c *DeploymentController) Create(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	return c.deployActiveVersion(ctx, app)
}

func (c *DeploymentController) Update(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	return c.deployActiveVersion(ctx, app)
}

func (c *DeploymentController) Delete(ctx context.Context, id entity.Id) error {
	// Nothing to do on delete - sandboxes will be cleaned up by their own lifecycle
	return nil
}

func (c *DeploymentController) deployActiveVersion(ctx context.Context, app *core_v1alpha.App) error {
	if app.ActiveVersion == "" {
		c.Log.Debug("app has no active version, nothing to deploy", "app", app.ID)
		return nil
	}

	// Get the app version details
	vr, err := c.EAC.Get(ctx, app.ActiveVersion.String())
	if err != nil {
		return fmt.Errorf("failed to get app version: %w", err)
	}

	var appVersion core_v1alpha.AppVersion
	appVersion.Decode(vr.Entity().Entity())

	c.Log.Info("deploying app version",
		"app", app.ID,
		"version", appVersion.Version,
		"min_instances", appVersion.Config.Concurrency.MinInstances)

	// Call the activator's Deploy method to handle the deployment
	if err := c.AA.Deploy(ctx, app, &appVersion, activator.DefaultPool); err != nil {
		return fmt.Errorf("failed to deploy app version: %w", err)
	}

	return nil
}
