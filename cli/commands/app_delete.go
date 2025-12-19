package commands

import (
	"fmt"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AppDelete(ctx *Context, opts struct {
	Force   bool   `short:"f" long:"force" description:"Force delete without confirmation"`
	AppName string `position:"0" usage:"Name of the app to delete" required:"true"`
	ConfigCentric
}) error {
	appName := opts.AppName

	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	crud := app_v1alpha.NewCrudClient(crudcl)

	// Confirm deletion unless forced
	if !opts.Force {
		confirmed, err := ui.Confirm(
			ui.WithMessage(fmt.Sprintf("Delete app '%s' and all its versions, artifacts, pools, and routes?", appName)),
			ui.WithDefault(false),
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Printf("Deletion cancelled\n")
			return nil
		}
	}

	_, err = crud.Destroy(ctx, appName)
	if err != nil {
		return err
	}

	ctx.Printf("Deleted app %s\n", appName)
	return nil
}
