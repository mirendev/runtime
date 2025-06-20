package commands

import (
	"fmt"

	apppkg "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/ingress"
)

// DefaultRouteSet creates or updates an http_route to be the default route for an app
func DefaultRouteSet(ctx *Context, opts struct {
	AppCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	appClient := apppkg.NewClient(ctx.Log, cl)
	ingressClient := ingress.NewClient(ctx.Log, cl)

	appName := opts.App

	// Get the app to ensure it exists
	app, err := appClient.GetByName(ctx, appName)
	if err != nil {
		return fmt.Errorf("failed to get app %s: %w", appName, err)
	}

	ctx.Log.Info("setting default route", "app", app.ID)

	_, err = ingressClient.SetDefault(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to set default route: %w", err)
	}

	// DefaultRouteController will handle ensuring single default
	ctx.Completed("Set the default route to: %s", app.ID)
	return nil
}

// DefaultRouteUnset removes the default flag from all http_routes
func DefaultRouteUnset(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	ingressClient := ingress.NewClient(ctx.Log, cl)

	oldDefault, err := ingressClient.UnsetDefault(ctx)
	if err != nil {
		return fmt.Errorf("failed to lookup default route: %w", err)
	}

	if oldDefault != nil {
		ctx.Completed("Removed default route from app: %s", oldDefault.App)
	} else {
		ctx.Completed("No default route is currently set")
	}

	return nil
}

// DefaultRouteShow shows which route is currently the default
func DefaultRouteShow(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	ingressClient := ingress.NewClient(ctx.Log, cl)

	defaultRoute, err := ingressClient.LookupDefault(ctx)
	if err != nil {
		return fmt.Errorf("failed to lookup default route: %w", err)
	}

	if defaultRoute == nil {
		ctx.Printf("No default route is currently set\n")
		return nil
	}

	ctx.Printf("Default route goes to app: %s\n", defaultRoute.App)

	return nil
}
