package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
)

func RouteUnprotect(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com); omit and pass --default for the default route"`
	Default bool   `long:"default" description:"Remove protection from the default route (instead of a hostname)"`
	ConfigCentric
}) error {
	if opts.Host == "" && !opts.Default {
		return fmt.Errorf("either a hostname or --default must be specified")
	}

	if opts.Host != "" && opts.Default {
		return fmt.Errorf("--default cannot be used with a hostname")
	}

	client, err := routeSettingsRPCClient(ctx)
	if err != nil {
		return err
	}

	ic := ingress_v1alpha.NewRoutesClient(client)
	res, err := ic.Show(ctx, opts.Host, opts.Default)
	if err != nil {
		return err
	}
	routeLabel := opts.Host
	if opts.Default {
		routeLabel = "default"
	}

	if !res.Route().Protected() {
		ctx.Printf("Route is not protected: %s\n", routeLabel)
		return nil
	}

	_, err = ic.Unprotect(ctx, opts.Host, opts.Default)
	if err != nil {
		return fmt.Errorf("failed to remove route protection: %w", err)
	}

	ctx.Printf("Protection removed from route: %s\n", routeLabel)
	return nil
}
