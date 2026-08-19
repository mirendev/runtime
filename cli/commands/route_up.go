package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
)

func RouteUp(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com); omit and pass --default for the default route"`
	Default bool   `long:"default" description:"Bring the default route back (instead of a hostname)"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Host == "" && !opts.Default {
		return fmt.Errorf("either a hostname or --default must be specified")
	}

	if opts.Host != "" && opts.Default {
		return fmt.Errorf("--default cannot be used with a hostname")
	}

	client, err := ctx.RPCClient("dev.miren.runtime/routes")
	if err != nil {
		return err
	}

	routes := ingress_v1alpha.NewRoutesClient(client)

	res, err := routes.ClearMaintenance(ctx, opts.Host, opts.Default)
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		type RouteUpJSON struct {
			Route       string `json:"route"`
			Maintenance bool   `json:"maintenance"`
			Changed     bool   `json:"changed"`
		}

		return PrintJSON(RouteUpJSON{
			Route:       res.Route(),
			Maintenance: false,
			Changed:     res.Changed(),
		})
	}

	// Bringing a route back that was never down succeeds rather than erroring.
	// The reversal path is the one an operator runs under pressure, so it
	// shouldn't make them stop and think.
	if !res.Changed() {
		ctx.Printf("Route %s was already serving normally.\n", res.Route())
		return nil
	}

	ctx.Printf("Route %s is serving normally.\n", res.Route())
	return nil
}
