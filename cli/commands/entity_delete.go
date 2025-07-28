package commands

import (
	"errors"
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
)

func EntityDelete(ctx *Context, opts struct {
	ConfigCentric
	Id      string `short:"i" long:"id" description:"Entity ID" required:"true"`
	Address string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	res, err := eac.Delete(ctx, opts.Id)
	if err != nil {
		ctx.Log.Error("failed to delete entity", "error", err)
		//If entity doesnt exist, consider the delete to be successful.
		if errors.Is(err, cond.ErrNotFound{}) {
			ctx.Log.Info("Entity already deleted or does not exist", "id", opts.Id)
			os.Stdout.WriteString("Entity deleted successfully.\n")
			return nil
		}
		return err
	}
	ctx.Log.Info("Entity deleted successfully", "id", opts.Id, "revision", res.HasRevision)
	os.Stdout.WriteString("Entity deleted successfully.\n")

	return nil

}
