package commands

import (
	"miren.dev/runtime/server"
)

func WhoAmI(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cl, err := ctx.RPCClient("user")
	if err != nil {
		return err
	}

	uq := &server.UserQueryClient{Client: cl}

	results, err := uq.WhoAmI(ctx)
	if err != nil {
		return err
	}

	ctx.Printf("%s\n", results.Info().Subject())
	return nil
}
