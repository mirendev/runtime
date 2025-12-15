package commands

import (
	"fmt"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/ingress"
)

func RouteSet(ctx *Context, opts struct {
	Host string `position:"0" usage:"Hostname for the route (e.g., example.com)" required:"true"`
	App  string `position:"1" usage:"Application name to route to" required:"true"`
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Look up the app by name
	appClient := app.NewClient(ctx.Log, client)
	appEntity, err := appClient.GetByName(ctx, opts.App)
	if err != nil {
		return fmt.Errorf("failed to find app %q: %w", opts.App, err)
	}

	// Create/update the route
	ic := ingress.NewClient(ctx.Log, client)
	_, err = ic.SetRoute(ctx, opts.Host, appEntity.ID)
	if err != nil {
		return err
	}

	ctx.Printf("Route set: %s → %s\n", opts.Host, opts.App)
	return nil
}
