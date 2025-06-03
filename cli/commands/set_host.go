package commands

import (
	"fmt"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
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

	ea := entityserver_v1alpha.NewEntityAccessClient(client)

	// Construct the app ID from the name
	appId := fmt.Sprintf("dev.miren.app/v1alpha/app/%s", opts.App)

	// Verify the app exists
	_, err = ea.Get(ctx, appId)
	if err != nil {
		return err
	}

	// Construct the route ID from the host
	routeId := fmt.Sprintf("dev.miren.ingress/v1alpha/http_route/%s", opts.Host)

	// Create the entity
	var e entityserver_v1alpha.Entity
	e.SetId(routeId)
	e.SetAttrs(entity.Attrs(
		entity.Ident, routeId,
		"app", appId,
		"host", opts.Host,
	))

	// Put the entity
	_, err = ea.Put(ctx, &e)
	if err != nil {
		return err
	}

	ctx.Printf("Host set to %s\n", opts.Host)
	return nil
}
