package commands

import (
	"miren.dev/runtime/app"
)

func SetHost(ctx *Context, opts struct {
	AppCentric
	Host string `long:"host" description:"Set host"`
}) error {
	cl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	ac := app.CrudClient{Client: cl}

	_, err = ac.SetHost(ctx, opts.App, opts.Host)
	return err
}
