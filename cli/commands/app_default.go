package commands

import (
	"fmt"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// AppDefaultSet sets an app as the default app
func AppDefaultSet(ctx *Context, opts struct {
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

	ctx.Log.Info("setting default app", "app", appName)

	// Get the app
	resp, err := eac.Get(ctx, appName)
	if err != nil {
		return fmt.Errorf("failed to get app %s: %w", appName, err)
	}

	var app core_v1alpha.App
	app.Decode(resp.Entity().Entity())

	// Check if already default
	if app.Default {
		ctx.Printf("App %s is already the default app\n", appName)
		return nil
	}

	// Set the app as default - DefaultAppController will handle reconciliation
	app.Default = true

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(string(app.ID))
	rpcE.SetAttrs(app.Encode())

	_, err = eac.Put(ctx, &rpcE)
	if err != nil {
		return fmt.Errorf("failed to set app %s as default: %w", appName, err)
	}

	ctx.Completed("Set %s as the default app", appName)
	return nil
}

// AppDefaultUnset removes the default flag from all apps
func AppDefaultUnset(ctx *Context, _opts struct {
	ConfigCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(cl)

	ctx.Log.Info("unsetting default app")

	// Find all apps that are currently marked as default
	resp, err := eac.List(ctx, entity.Bool(core_v1alpha.AppDefaultId, true))
	if err != nil {
		return fmt.Errorf("failed to list default apps: %w", err)
	}

	if len(resp.Values()) == 0 {
		ctx.Printf("No default app is currently set\n")
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
			return fmt.Errorf("failed to unset default flag from app %s: %w", app.ID, err)
		}

		ctx.Completed("Removed default flag from %s", app.ID)
	}

	return nil
}

// AppDefaultShow shows which app is currently the default
func AppDefaultShow(ctx *Context, _opts struct {
	ConfigCentric
}) error {
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
		ctx.Printf("No default app is currently set\n")
		return nil
	}

	for _, ent := range resp.Values() {
		var app core_v1alpha.App
		app.Decode(ent.Entity())

		var metadata core_v1alpha.Metadata
		metadata.Decode(ent.Entity())

		ctx.Printf("Default app: %s\n", metadata.Name)
	}

	return nil
}
