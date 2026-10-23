package commands

import (
	"fmt"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/compute/compute_v1alpha"
)

func SandboxDelete(ctx *Context, opts struct {
	Force     bool   `short:"f" long:"force" description:"Force delete without confirmation"`
	SandboxID string `position:"0" usage:"ID of the sandbox to delete" required:"true"`
	ConfigCentric
}) error {
	sandboxID := opts.SandboxID

	client, err := ctx.RPCClient("dev.miren.runtime/sandboxes")
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(ctx, client, "delete")
	}
	if err != nil && !isMissingRPC(err) {
		return err
	}

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

	if err == nil {
		_, err = compute_v1alpha.NewSandboxesClient(client).Delete(ctx, sandboxID)
	}
	if isMissingRPC(err) {
		legacy, lookupErr := ctx.RPCClient("entities")
		if lookupErr != nil {
			return lookupErr
		}
		defer legacy.Close()
		err = compute.NewClient(ctx.Log, legacy).DeleteSandbox(ctx, sandboxID)
	}
	if err != nil {
		return err
	}

	ctx.Printf("Deleted sandbox %s\n", sandboxID)
	return nil
}
