package commands

import (
	"fmt"

	"miren.dev/runtime/api/app/app_v1alpha"
)

func AppNew(ctx *Context, opts struct {
	Name string `short:"n" long:"name" description:"Name of the app"`
}) error {
	if opts.Name == "" {
		return fmt.Errorf("name is required")
	}

	cl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	ac := app_v1alpha.CrudClient{Client: cl}

	results, err := ac.New(ctx, opts.Name)
	if err != nil {
		return err
	}

	ctx.Printf("app id: %s\n", results.Id())

	return nil
}
