package commands

import (
	"miren.dev/runtime/api/compute"
)

func SandboxStop(ctx *Context, opts struct {
	ConfigCentric
	Args struct {
		SandboxID string `positional-arg-name:"sandbox-id" description:"ID of the sandbox to stop"`
	} `positional-args:"yes" required:"yes"`
}) error {
	sandboxID := opts.Args.SandboxID

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	computeClient := compute.NewClient(ctx.Log, client)

	err = computeClient.StopSandbox(ctx, sandboxID)
	if err != nil {
		return err
	}

	ctx.Printf("Stopped sandbox %s\n", sandboxID)
	return nil
}
