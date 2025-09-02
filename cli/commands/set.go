package commands

import (
	"miren.dev/runtime/api/app/app_v1alpha"
)

func Set(ctx *Context, opts struct {
	AppCentric
	Concurrency int `short:"c" long:"concurrency" description:"Set maximum concurrency of application instances" required:"true"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewCrudClient(cl)

	res, err := ac.GetConfiguration(ctx, opts.App)
	if err != nil {
		return err
	}

	cfg := res.Configuration()

	if cfg.Concurrency() == int32(opts.Concurrency) {
		ctx.Printf("concurrency is already set to %d\n", opts.Concurrency)
		return nil
	}

	ctx.Printf("setting concurrency to %d...\n", opts.Concurrency)
	cfg.SetConcurrency(int32(opts.Concurrency))

	setres, err := ac.SetConfiguration(ctx, opts.App, cfg)
	if err != nil {
		return err
	}

	ctx.Printf("new version id: %s\n", setres.VersionId())

	return nil
}
