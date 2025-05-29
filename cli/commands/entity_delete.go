package commands

import (
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"os"
)

func EntityDelete(ctx *Context, opts struct {
	Id      string `short:"i" long:"id" description:"Entity ID" required:"true"`
	Address string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
}) error {
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		ctx.Log.Error("failed to create rpc state", "error", err)
		return err
	}

	client, err := rs.Connect(opts.Address, "entities")
	if err != nil {
		ctx.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	res, err := eac.Delete(ctx, opts.Id)
	if err != nil {
		ctx.Log.Info("failed to delete entity", "error", err)
		return err
	}
	ctx.Log.Info("Entity deleted successfully", "id", opts.Id, "revision", res.HasRevision)
	os.Stdout.WriteString("Entity deleted successfully.\n")

	return nil

}
