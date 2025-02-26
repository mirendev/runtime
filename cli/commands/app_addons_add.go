package commands

import "miren.dev/runtime/api"

func AppAddonsAdd(ctx *Context, opts struct {
	AppCentric
	Addon string `long:"addon" description:"The addon to add" required:"true"`
}) error {
	addonscl, err := ctx.RPCClient("addons")
	if err != nil {
		return err
	}

	cl := api.AddonsClient{Client: addonscl}

	res, err := cl.CreateInstance(ctx, "", opts.Addon, "", opts.App)
	if err != nil {
		return err
	}

	ctx.Info("Addon created: %s", res.Id())

	return nil
}
