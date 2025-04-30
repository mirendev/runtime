package commands

import (
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func EntityGet(ctx *Context, opts struct {
	Id      string `short:"i" long:"id" description:"Entity ID" required:"true"`
	Address string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
}) error {
	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		ctx.Log.Error("failed to create RPC client", "error", err)
		return err
	}

	client, err := rs.Connect(opts.Address, "entities")
	if err != nil {
		ctx.Log.Error("failed to connect to RPC server", "error", err)
		return err
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
