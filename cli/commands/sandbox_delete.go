package commands

import (
	"fmt"

	"miren.dev/runtime/api/compute"
)

func SandboxDelete(ctx *Context, opts struct {
	Force bool `short:"f" long:"force" description:"Force delete without confirmation"`
	ConfigCentric
	Args struct {
		SandboxID string `positional-arg-name:"sandbox-id" description:"ID of the sandbox to delete"`
	} `positional-args:"yes" required:"yes"`
}) error {
	sandboxID := opts.Args.SandboxID

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	computeClient := compute.NewClient(ctx.Log, client)

	// Confirm deletion unless forced
	if !opts.Force {
		ctx.Printf("Are you sure you want to delete sandbox %s? (y/N): ", sandboxID)
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			ctx.Printf("Deletion cancelled\n")
			return nil
		}
	}

	err = computeClient.DeleteSandbox(ctx, sandboxID)
	if err != nil {
		return err
	}

	ctx.Printf("Deleted sandbox %s\n", sandboxID)
	return nil
}
