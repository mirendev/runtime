package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
)

type RouteSetOptions struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com or *.example.com)" required:"true"`
	AppName string `position:"1" usage:"Deprecated: use --app instead"`
	Service string `long:"service" description:"HTTP-capable app service to route to" default:"web"`
	AppCentric
}

func (o *RouteSetOptions) Validate(flags *GlobalFlags) error {
	if o.AppName != "" {
		if o.App != "" && o.App != o.AppName {
			return fmt.Errorf("positional app %q disagrees with --app or MIREN_APP %q", o.AppName, o.App)
		}
		o.App = o.AppName
	}
	if err := o.AppCentric.Validate(flags); err != nil {
		return err
	}
	if o.AppName != "" {
		// Sandbox-injected app variables must not replace an explicit argument.
		o.App = o.AppName
	}
	return nil
}

func RouteSet(ctx *Context, opts RouteSetOptions) error {
	if opts.AppName != "" {
		fmt.Fprintln(os.Stderr, "Warning: the positional app in 'miren route set' is deprecated; use --app instead.")
	}
	if opts.Host == "" {
		return fmt.Errorf("route hostname is required")
	}

	if err := ingress.ValidateWildcardHost(opts.Host); err != nil {
		return err
	}

	appName := opts.App
	if appName == "" {
		appName = opts.AppName
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
