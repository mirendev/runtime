package commands

import (
	"miren.dev/runtime/build"
	"miren.dev/runtime/pkg/rpc/stream"
)

func Run(ctx *Context, opts struct {
	App string `short:"a" long:"app" description:"Application to run"`
	Dir string `short:"d" long:"dir" description:"Directory to run from"`
}) error {

	c := &cliRun{}

	id, err := c.buildCode(ctx, opts.App, opts.Dir)
	if err != nil {
		return err
	}

	ctx.Printf("built code with id %s", id)

	return nil
}

type cliRun struct{}

func (c *cliRun) buildCode(ctx *Context, name, dir string) (string, error) {
	cl, err := ctx.RPCClient("build")
	if err != nil {
		return "", err
	}

	bc := build.BuilderClient{Client: cl}

	ctx.Log.Info("building code", "name", name, "dir", dir)

	r, err := build.MakeTar(dir)
	if err != nil {
		return "", err
	}

	results, err := bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r))
	if err != nil {
		return "", err
	}

	return results.Version(), nil
}
