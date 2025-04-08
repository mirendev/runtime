package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/davecgh/go-spew/spew"
	"miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

func EntityPut(ctx *Context, opts struct {
	Address string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	Path    string `short:"p" long:"path" description:"Path to the entity"`
	Id      string `short:"i" long:"id" description:"ID of the entity"`
	DryRun  bool   `short:"d" long:"dry-run" description:"Dry run, do not actually put the entity"`
}) error {
	var (
		data []byte
		err  error
	)

	if opts.Path != "" {
		data, err = os.ReadFile(opts.Path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %v", opts.Path, err)
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %v", err)
		}
	}

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

	eac := &v1alpha.EntityAccessClient{Client: client}

	pres, err := eac.Parse(ctx, data)
	if err != nil {
		return fmt.Errorf("failed to parse entity: %v", err)
	}

	ent := pres.Entity().Entity()

	if opts.Id != "" {
		ent.Remove(entity.Ident)
		ent.Attrs = append(ent.Attrs, entity.Attrs(
			entity.Ident, opts.Id,
		)...)
	}

	var rpcE v1alpha.Entity

	rpcE.SetAttrs(ent.Attrs)

	spew.Dump(ent.Attrs)

	if opts.DryRun {
		ctx.Log.Info("Dry run, not putting entity")
		return nil
	}

	res, err := eac.Put(ctx, &rpcE)
	if err != nil {
		return fmt.Errorf("failed to put entity: %v", err)
	}

	ctx.Log.Info("Entity put successfully", "id", res.Id, "revision", res.Revision)

	return nil
}
