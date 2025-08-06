package commands

import (
	"fmt"

	"miren.dev/runtime/api/compute"
)

func SandboxStop(ctx *Context, opts struct {
	ConfigCentric
}, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: sandbox stop <sandbox-id>")
	}

	sandboxID := args[0]

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
