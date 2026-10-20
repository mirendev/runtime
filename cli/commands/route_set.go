package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
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
		if ac := loadLocalAppConfigOrWarn(); ac != nil && ac.Name != "" {
			appName = ac.Name
		}
	}
	if appName == "" {
		return fmt.Errorf("app is required")
	}

	client, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}

	routes := ingress_v1alpha.NewRoutesClient(client)
	_, err = routes.Set(ctx, opts.Host, appName, opts.Service)
	if err != nil {
		return err
	}

	ctx.Printf("Route set: %s → %s (%s)\n", opts.Host, appName, opts.Service)
	return nil
}
