package commands

import (
	"fmt"

	"miren.dev/runtime/api/app"
	compute "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/appconfig"
)

func RouteSet(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com or *.example.com)" required:"true"`
	AppName string `position:"1" usage:"Application name to route to"`
	Service string `long:"service" description:"HTTP-capable app service to route to" default:"web"`
	ConfigCentric
}) error {
	if opts.Host == "" {
		return fmt.Errorf("host is required")
	}

	if err := ingress.ValidateWildcardHost(opts.Host); err != nil {
		return err
	}

	appName := opts.AppName
	if appName == "" {
		ac, err := appconfig.LoadAppConfig()
		if err != nil {
			printConfigWarning(err)
		} else if ac != nil && ac.Name != "" {
			appName = ac.Name
		}
	}
	if appName == "" {
		return fmt.Errorf("app is required")
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Look up the app by name
	appClient := app.NewClient(ctx.Log, client)
	appEntity, err := appClient.GetByName(ctx, appName)
	if err != nil {
		return fmt.Errorf("failed to find app %q: %w", appName, err)
	}

	if appEntity.ActiveVersion == "" {
		return fmt.Errorf("app %q has no active configuration", appName)
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	version, err := eac.Get(ctx, appEntity.ActiveVersion.String())
	if err != nil {
		return fmt.Errorf("failed to get active configuration for app %q: %w", appName, err)
	}
	var appVersion core_v1alpha.AppVersion
	appVersion.Decode(version.Entity().Entity())
	spec, err := compute.ResolveRuntimeConfig(ctx, eac, &appVersion)
	if err != nil {
		return fmt.Errorf("failed to resolve active configuration for app %q: %w", appName, err)
	}
	if err := ingress.HTTPService(spec, opts.Service); err != nil {
		return err
	}

	// Create/update the route
	ic := ingress.NewClient(ctx.Log, client)
	_, err = ic.SetRoute(ctx, opts.Host, appEntity.ID, opts.Service)
	if err != nil {
		return err
	}

	ctx.Printf("Route set: %s → %s (%s)\n", opts.Host, appName, opts.Service)
	return nil
}
