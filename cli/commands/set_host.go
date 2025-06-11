package commands

import (
	"fmt"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"net"
)

func SetHost(ctx *Context, opts struct {
	ConfigCentric
	App  string `short:"a" long:"app" description:"Application name" required:"false"`
	Host string `short:"h" long:"host" description:"Set host" required:"true"`
}) error {
	var (
		rs     *rpc.State
		client *rpc.NetworkClient
	)
	_, err := net.LookupIP(opts.Host)
	if err != nil {
		ctx.Printf("Warning: DNS lookup failed for host %q: %v\n", opts.Host, err)
		return err
	}

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

	appName := opts.App

	if appName == "" {
		var app core_v1alpha.App
		eac := entityserver_v1alpha.NewEntityAccessClient(client)
		// Find app marked as default
		resp, err := eac.List(ctx, entity.Bool(core_v1alpha.AppDefaultId, true))
		if err != nil {
			return fmt.Errorf("failed to list default app: %w", err)
		}

		if len(resp.Values()) == 0 {
			return fmt.Errorf("No default app is currently set. Use app default set <app>.")
			return nil
		}

		for _, ent := range resp.Values() {
			app.Decode(ent.Entity())

			var metadata core_v1alpha.Metadata
			metadata.Decode(ent.Entity())
			appName = metadata.Name
		}
		ctx.Printf("Using default app: %s\n", appName)

	}

	appClient, err := app.NewClient(ctx, ctx.Log, client)
	if err != nil {
		return err
	}

	err = appClient.SetHost(ctx, appName, opts.Host)
	if err != nil {
		return err
	}

	ctx.Printf("Host set to %s\n", opts.Host)
	return nil
}
