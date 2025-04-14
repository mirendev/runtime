package commands

import (
	"os"

	"github.com/davecgh/go-spew/spew"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

func EntityList(ctx *Context, opts struct {
	Attribute string `short:"a" long:"attribute" description:"Attribute to filter by"`
	Value     string `short:"v" long:"value" description:"Value to filter by"`
	Kind      string `short:"k" long:"kind" description:"Kind of entity to filter by"`
	Address   string `long:"address" description:"Address to listen on" default:"localhost:8443"`
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

	eac := &entityserver_v1alpha.EntityAccessClient{Client: client}

	var index entity.Attr

	if opts.Kind != "" {
		res, err := eac.LookupKind(ctx, opts.Kind)
		if err != nil {
			return err
		}

		index = res.Attr()
	} else {
		indexres, err := eac.MakeAttr(ctx, opts.Attribute, opts.Value)
		if err != nil {
			return err
		}

		index = indexres.Attr()
	}

	spew.Dump(index)

	res, err := eac.List(ctx, index)
	if err != nil {
		return err
	}

	for _, e := range res.Values() {
		fres, err := eac.Format(ctx, e)
		if err != nil {
			return err
		}

		os.Stdout.Write(fres.Data())
	}

	return nil
}
