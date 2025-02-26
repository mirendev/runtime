package commands

import (
	"miren.dev/runtime/api"
	"miren.dev/runtime/pkg/slicex"
)

func AppAddonsList(ctx *Context, opts struct {
	AppCentric
}) error {
	addonscl, err := ctx.RPCClient("addons")
	if err != nil {
		return err
	}

	cl := api.AddonsClient{Client: addonscl}

	res, err := cl.ListInstances(ctx, opts.App)
	if err != nil {
		return err
	}

	if len(res.Addons()) == 0 {
		ctx.Info("No addons found")
		return nil
	}

	// Print header

	// Print each addon

	ctx.DisplayTableTemplate(
		"Name:Name,Addon:Addon,Plan:Plan",
		slicex.ToAny(res.Addons()),
	)

	/*
		for _, addon := range res.Addons() {
			ctx.Info("%s\t%s\t%s\n",
				addon.Id(),
				addon.Addon(),
				addon.Plan(),
			)
		}
	*/

	return nil
}
