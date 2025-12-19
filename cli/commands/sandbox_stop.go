package commands

import (
	"miren.dev/runtime/api/compute"
)

func SandboxStop(ctx *Context, opts struct {
	SandboxID string `position:"0" usage:"ID of the sandbox to stop" required:"true"`
	ConfigCentric
}) error {
	sandboxID := opts.SandboxID

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
