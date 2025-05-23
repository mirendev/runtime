package commands

import (
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

func EntityList(ctx *Context, opts struct {
	Attribute string `short:"a" long:"attribute" description:"Attribute to filter by"`
	Value     string `short:"v" long:"value" description:"Value to filter by"`
	Kind      string `short:"k" long:"kind" description:"Kind of entity to filter by"`
	Address   string `long:"address" description:"Address to listen on" default:"localhost:8443"`

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

	res, err := eac.List(ctx, index)
	if err != nil {
		return err
	}

	for i, e := range res.Values() {
		if i > 0 {
			os.Stdout.Write([]byte("---\n"))
		}
		fres, err := eac.Format(ctx, e)
		if err != nil {
			return err
		}

		os.Stdout.Write(fres.Data())
	}

	return nil
}
