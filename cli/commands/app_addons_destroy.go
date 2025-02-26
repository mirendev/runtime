package commands

import "miren.dev/runtime/api"

func AppAddonsDestroy(ctx *Context, opts struct {
	AppCentric
	Name string `long:"addon" description:"The addon name to destroy" required:"true"`
}) error {
	addonscl, err := ctx.RPCClient("addons")
	if err != nil {
		return err
	}

	cl := api.AddonsClient{Client: addonscl}

	_, err = cl.DeleteInstance(ctx, opts.App, opts.Name)
	if err != nil {
		return err
	}

	ctx.Info("Addon destroyed: %s", opts.Name)

	return nil
}
