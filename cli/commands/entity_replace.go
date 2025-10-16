package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func EntityReplace(ctx *Context, opts struct {
	ConfigCentric
	Address  string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	Path     []string `short:"p" long:"path" description:"Path to the entity file"`
	Id       string   `short:"i" long:"id" description:"ID of the entity (required)" required:"true"`
	Revision int64    `short:"r" long:"revision" description:"Expected revision for optimistic concurrency"`
	DryRun   bool     `short:"d" long:"dry-run" description:"Dry run, do not actually replace the entity"`
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
			return fmt.Errorf("failed to read file %s: %v", path, err)
		}

		press, err := eac.Parse(ctx, data)
		if err != nil {
			return fmt.Errorf("failed to parse entity: %v", err)
		}

		pf := press.File()

		for _, rpcEnt := range pf.Entities() {
			ent := rpcEnt.Entity()

			// Add db/id attribute with the provided ID
			attrs := append(ent.Attrs(), entity.Attr{
				ID:    entity.DBId,
				Value: entity.AnyValue(entity.Id(opts.Id)),
			})

			if opts.DryRun {
				ctx.Log.Info("Dry run, not replacing entity", "id", opts.Id, "attrs", attrs)
				continue
			}

			res, err := eac.Replace(ctx, attrs, opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to replace entity: %v", err)
			}

			ctx.Log.Info("Entity replaced successfully", "id", res.Id, "revision", res.Revision)
		}
	}

	return nil
}
