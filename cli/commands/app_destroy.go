package commands

import "miren.dev/runtime/app"

func AppDestroy(ctx *Context, opts struct {
	AppCentric
	Confirm bool `short:"c" long:"confirm" description:"Confirm the destruction of the app"`
}) error {
	crudcl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	crud := app.CrudClient{Client: crudcl}

	if !opts.Confirm {
		ctx.Info("Please confirm the destruction of the app with --confirm\n")
		return nil
	}

	_, err = crud.Destroy(ctx, opts.App)
	if err != nil {
		return err
	}

	ctx.Completed("App %s destroyed", opts.App)
	return nil
}
