package commands

import (
	"miren.dev/runtime/api/app/app_v1alpha"
)

func AppDisable(ctx *Context, opts struct {
	Reason string `short:"r" long:"reason" description:"Why the app is disabled, shown by app status"`
	AppCentric
}) error {
	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	result, err := app_v1alpha.NewCrudClient(crudcl).Disable(ctx, opts.App, opts.Reason)
	if err != nil {
		return err
	}

	ctx.Printf("Disabled app %s (scaled %d pools to zero)\n", opts.App, result.ScaledPools())
	ctx.Printf("Run `miren app enable -a %s` to start it again.\n", opts.App)
	return nil
}

func AppEnable(ctx *Context, opts struct {
	AppCentric
}) error {
	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	result, err := app_v1alpha.NewCrudClient(crudcl).Enable(ctx, opts.App)
	if err != nil {
		return err
	}

	ctx.Printf("Enabled app %s (restored %d fixed pools; autoscaled services start on the next request)\n",
		opts.App, result.RestoredPools())
	return nil
}
