package commands

import (
	"fmt"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/ingress"
)

func RouteSet(ctx *Context, opts struct {
	ConfigCentric
	Args struct {
		Host string `positional-arg-name:"host" description:"Hostname for the route (e.g., example.com)" required:"true"`
		App  string `positional-arg-name:"app" description:"Application name to route to" required:"true"`
	} `positional-args:"yes"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Look up the app by name
	appClient := app.NewClient(ctx.Log, client)
	appEntity, err := appClient.GetByName(ctx, opts.Args.App)
	if err != nil {
		return fmt.Errorf("failed to find app %q: %w", opts.Args.App, err)
	}

	// Create/update the route
	ic := ingress.NewClient(ctx.Log, client)
	_, err = ic.SetRoute(ctx, opts.Args.Host, appEntity.ID)
	if err != nil {
		return err
	}

	ctx.Printf("Route set: %s → %s\n", opts.Args.Host, opts.Args.App)
	return nil
}
