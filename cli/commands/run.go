package commands

import (
	"context"
	"time"

	"miren.dev/runtime/build"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

func Run(ctx *Context, opts struct {
	App string `short:"a" long:"app" description:"Application to run"`
}) error {
	ctx.Log.DebugContext(ctx, "running application")

	t := time.NewTimer(5 * time.Minute)

	select {
	case <-ctx.Done():
		return nil
	case <-t.C:
		return nil
	}
}

type cliRun struct {
	c *rpc.Client
}

func (c *cliRun) buildCode(ctx context.Context, name, dir string) (string, error) {
	bc := build.BuilderClient{Client: c.c}

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
