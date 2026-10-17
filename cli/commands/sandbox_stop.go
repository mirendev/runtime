package commands

import (
	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/compute/compute_v1alpha"
)

func SandboxStop(ctx *Context, opts struct {
	SandboxID string `position:"0" usage:"ID of the sandbox to stop" required:"true"`
	ConfigCentric
}) error {
	sandboxID := opts.SandboxID

	client, err := ctx.RPCClient("dev.miren.runtime/sandboxes")
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(ctx, client, "stop")
	}
	if err != nil && !isMissingRPC(err) {
		return err
	}

	if err == nil {
		_, err = compute_v1alpha.NewSandboxesClient(client).Stop(ctx, sandboxID)
	}
	if isMissingRPC(err) {
		legacy, lookupErr := ctx.RPCClient("entities")
		if lookupErr != nil {
			return lookupErr
		}
		defer legacy.Close()
		err = compute.NewClient(ctx.Log, legacy).StopSandbox(ctx, sandboxID)
	}
	if err != nil {
		return err
	}

	ctx.Printf("Stopped sandbox %s\n", sandboxID)
	return nil
}
