package commands

import (
	"fmt"

	apppkg "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/ingress"
)

func RouteSetDefault(ctx *Context, opts struct {
	ConfigCentric
	Args struct {
		App string `positional-arg-name:"app" description:"Application name to set as default route" required:"true"`
	} `positional-args:"yes"`
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	appClient := apppkg.NewClient(ctx.Log, cl)
	ingressClient := ingress.NewClient(ctx.Log, cl)

	// Get the app to ensure it exists
	app, err := appClient.GetByName(ctx, opts.Args.App)
	if err != nil {
		return fmt.Errorf("failed to get app %s: %w", opts.Args.App, err)
	}

	ctx.Log.Info("setting default route", "app", app.ID)

	_, err = ingressClient.SetDefault(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to set default route: %w", err)
	}

	ctx.Printf("Set default route to: %s\n", opts.Args.App)
	return nil
}
