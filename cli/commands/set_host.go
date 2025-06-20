package commands

import (
	"miren.dev/runtime/api/app"
)

func SetHost(ctx *Context, opts struct {
	ConfigCentric
	App  string `short:"a" long:"app" description:"Application name" required:"true"`
	Host string `short:"h" long:"host" description:"Set host" required:"true"`
}) error {

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create app client
	appClient := app.NewClient(ctx.Log, client)

	// Set the host for the app
	err = appClient.SetHost(ctx, opts.App, opts.Host)
	if err != nil {
		return err
	}

	ctx.Printf("Host set to %s\n", opts.Host)
	return nil
}
