package commands

import (
	"fmt"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// DefaultRouteSet sets an app as the default route
func DefaultRouteSet(ctx *Context, opts struct {
	AppCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(cl)

	appName := opts.App
	if appName == "" {
		return fmt.Errorf("app name is required")
	}

	ctx.Log.Info("setting default route", "app", appName)

	// Get the app
	resp, err := eac.Get(ctx, appName)
	if err != nil {
		return fmt.Errorf("failed to get app %s: %w", appName, err)
	}

	var app core_v1alpha.App
	app.Decode(resp.Entity().Entity())

	// Check if already default
	if app.Default {
		ctx.Printf("App %s is already the default route\n", appName)
		return nil
	}

	// Set the app as default - DefaultRouteController will handle reconciliation
	app.Default = true

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(string(app.ID))
	rpcE.SetAttrs(app.Encode())

	_, err = eac.Put(ctx, &rpcE)
	if err != nil {
		return fmt.Errorf("failed to set app %s as default route: %w", appName, err)
	}

	ctx.Completed("Set %s as the default route", appName)
	return nil
}

// DefaultRouteUnset removes the default flag from all apps
func DefaultRouteUnset(ctx *Context, opts struct{}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(cl)

	ctx.Log.Info("unsetting default route")

	// Find all apps that are currently marked as default
	resp, err := eac.List(ctx, entity.Bool(core_v1alpha.AppDefaultId, true))
	if err != nil {
		return fmt.Errorf("failed to list default apps: %w", err)
	}

	if len(resp.Values()) == 0 {
		ctx.Printf("No default route is currently set\n")
		return nil
	}

	for _, ent := range resp.Values() {
		var app core_v1alpha.App
		app.Decode(ent.Entity())

		ctx.Log.Info("removing default flag from app", "app", app.ID)
		app.Default = false

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(string(app.ID))
		rpcE.SetAttrs(app.Encode())

		_, err := eac.Put(ctx, &rpcE)
		if err != nil {
			return fmt.Errorf("failed to unset default route flag from app %s: %w", app.ID, err)
		}

		ctx.Completed("Removed default route flag from %s", app.ID)
	}

	return nil
}

// DefaultRouteShow shows which app is currently the default route
func DefaultRouteShow(ctx *Context, opts struct{}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(cl)

	// Find apps that are currently marked as default
	resp, err := eac.List(ctx, entity.Bool(core_v1alpha.AppDefaultId, true))
	if err != nil {
		return fmt.Errorf("failed to list default apps: %w", err)
	}

	if len(resp.Values()) == 0 {
		ctx.Printf("No default route is currently set\n")
		return nil
	}

	for _, ent := range resp.Values() {
		var app core_v1alpha.App
		app.Decode(ent.Entity())

		var metadata core_v1alpha.Metadata
		metadata.Decode(ent.Entity())

		ctx.Printf("Default route: %s\n", metadata.Name)
	}

	return nil
}
