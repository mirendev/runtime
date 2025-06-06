package commands

import (
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/pkg/rpc"
)

func SetHost(ctx *Context, opts struct {
	ConfigCentric
	App  string `short:"a" long:"app" description:"Application name" required:"true"`
	Host string `short:"h" long:"host" description:"Set host" required:"true"`
}) error {
	var (
		rs     *rpc.State
		client *rpc.NetworkClient
	)

	cc, err := opts.LoadConfig()
	if err != nil {
		addr := "localhost:8443"

		rs, err = rpc.NewState(ctx, rpc.WithSkipVerify)
		if err != nil {
			return err
		}

		client, err = rs.Connect(addr, "entities")
		if err != nil {
			return err
		}
	} else {
		rs, err = cc.State(ctx, rpc.WithLogger(ctx.Log))
		if err != nil {
			return err
		}

		client, err = rs.Client("entities")
		if err != nil {
			return err
		}
	}

	// Create app client
	appClient, err := app.NewClient(ctx, ctx.Log, client)
	if err != nil {
		return err
	}

	// Set the host for the app
	err = appClient.SetHost(ctx, opts.App, opts.Host)
	if err != nil {
		return err
	}

	ctx.Printf("Host set to %s\n", opts.Host)
	return nil
}
