package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func EntityCreate(ctx *Context, opts struct {
	ConfigCentric
	Address string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	Path    []string `short:"p" long:"path" description:"Path to the entity file"`
	Id      string   `short:"i" long:"id" description:"ID of the entity (optional, auto-generated if not provided)"`
	DryRun  bool     `short:"d" long:"dry-run" description:"Dry run, do not actually create the entity"`
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

			attrs := ent.Attrs()

			// Add db/id attribute if ID is provided
			if rpcEnt.HasId() {
				ctx.Log.Info("Entity has ID, using it", "id", rpcEnt.Id())
				attrs = append(attrs, entity.Attr{
					ID:    entity.DBId,
					Value: entity.AnyValue(entity.Id(rpcEnt.Id())),
				})
			} else if opts.Id != "" {
				attrs = append(attrs, entity.Attr{
					ID:    entity.DBId,
					Value: entity.AnyValue(entity.Id(opts.Id)),
				})
			}

			if opts.DryRun {
				ctx.Log.Info("Dry run, not creating entity", "attrs", attrs)
				continue
			}

			res, err := eac.Create(ctx, attrs)
			if err != nil {
				return fmt.Errorf("failed to create entity: %v", err)
			}

			ctx.Log.Info("Entity created successfully", "id", res.Id, "revision", res.Revision)
		}
	}

	return nil
}
