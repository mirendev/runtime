package commands

import (
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

func EntityGet(ctx *Context, opts struct {
	Id      string `short:"i" long:"id" description:"Entity ID" required:"true"`
	Address string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`

	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
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
