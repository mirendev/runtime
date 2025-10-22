package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func EntityPut(ctx *Context, opts struct {
	ConfigCentric
	Address string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	Path    []string `short:"p" long:"path" description:"Path to the entity"`
	Id      string   `short:"i" long:"id" description:"ID of the entity"`
	DryRun  bool     `short:"d" long:"dry-run" description:"Dry run, do not actually put the entity"`
	Update  bool     `short:"u" long:"update" description:"Update the entity if it exists"`
}) error {
	var (
		data []byte
		err  error
	)

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	for _, path := range opts.Path {
		data, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %v", opts.Path, err)
		}

		press, err := eac.Parse(ctx, data)
		if err != nil {
			return fmt.Errorf("failed to parse entity: %v", err)
		}

		pf := press.File()

		for _, rpcEnt := range pf.Entities() {
			ent := rpcEnt.Entity()

			var rpcE entityserver_v1alpha.Entity

			if rpcEnt.HasId() {
				ctx.Log.Info("Entity has ID, using it", "id", rpcEnt.Id())
				rpcE.SetId(rpcEnt.Id())
			} else if opts.Id != "" {
				rpcE.SetId(opts.Id)
				ent.Remove(entity.Ident)
			}

			rpcE.SetAttrs(ent.Attrs())

			if opts.DryRun {
				ctx.Log.Info("Dry run, not putting entity")
				return nil
			}

			res, err := eac.Ensure(ctx, ent.Attrs())
			if err != nil {
				return fmt.Errorf("failed to put entity: %v", err)
			}

			if res.Created() {
				ctx.Log.Info("Entity created", "id", res.Id, "revision", res.Revision)
			} else {
				rres, err := eac.Replace(ctx, ent.Attrs(), res.Revision())
				if err != nil {
					return fmt.Errorf("failed to update entity: %v", err)
				}

				ctx.Log.Info("Entity updated", "id", res.Id, "revision", rres.Revision)
			}

			ctx.Log.Info("Entity put successfully", "id", res.Id, "revision", res.Revision)
		}
	}

	return nil
}
