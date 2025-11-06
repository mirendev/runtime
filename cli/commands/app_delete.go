package commands

import (
	"miren.dev/runtime/api/app"
)

func AppDelete(ctx *Context, opts struct {
	AppCentric
}) error {
	rpcClient, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	appClient := app.NewClient(ctx.Log, rpcClient)

	err = appClient.Destroy(ctx, opts.App)
	if err != nil {
		return err
	}

	ctx.Printf("App '%s' has been deleted\n", opts.App)
	return nil
}
