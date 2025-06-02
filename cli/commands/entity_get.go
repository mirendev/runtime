package commands

import (
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func EntityGet(ctx *Context, opts struct {
	Id      string `short:"i" long:"id" description:"Entity ID" required:"true"`
	Address string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`

	ConfigCentric
}) error {
	var (
		rs     *rpc.State
		client *rpc.NetworkClient
	)

	cc, err := opts.LoadConfig()
	if err != nil {
		addr := opts.Address
		if addr == "" {
			addr = "localhost:8443"
		}

		rs, err = rpc.NewState(ctx, rpc.WithSkipVerify)
		if err != nil {
			ctx.Log.Error("failed to create RPC client", "error", err)
			return err
		}

		// Create RPC client to interact with coordinator
		client, err = rs.Connect(addr, "entities")
		if err != nil {
			ctx.Log.Error("failed to connect to RPC server", "error", err)
			return err
		}

	} else {
		rs, err = cc.State(ctx, rpc.WithLogger(ctx.Log))
		if err != nil {
			ctx.Log.Error("failed to create RPC client", "error", err)
			return err
		}

		// Create RPC client to interact with coordinator
		client, err = rs.Client("entities")
		if err != nil {
			ctx.Log.Error("failed to connect to RPC server", "error", err)
			return err
		}
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	res, err := eac.Get(ctx, opts.Id)
	if err != nil {
		return err
	}

	fres, err := eac.Format(ctx, res.Entity())
	if err != nil {
		return err
	}

	os.Stdout.Write(fres.Data())

	return nil
}
