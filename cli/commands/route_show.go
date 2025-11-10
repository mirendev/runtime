package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/ui"
)

func RouteShow(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Args struct {
		Host string `positional-arg-name:"host" description:"Hostname of the route to show" required:"true"`
	} `positional-args:"yes"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	route, err := ic.Lookup(ctx, opts.Args.Host)
	if err != nil {
		return err
	}

	if route == nil {
		return fmt.Errorf("route not found: %s", opts.Args.Host)
	}

	if opts.IsJSON() {
		return PrintJSON(route)
	}

	// Display route information
	ctx.Printf("Route: %s\n", opts.Args.Host)
	ctx.Printf("  App:     %s\n", ui.CleanEntityID(string(route.App)))
	ctx.Printf("  Default: %v\n", route.Default)

	// Note: We don't have created/updated timestamps from Lookup
	// If we need those, we'd need to add a GetWithMeta method

	return nil
}
