package commands

import (
	"fmt"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

// DiskAcceleratorInstall asks the cluster to build and load the lbd kernel
// module on a node, so its disks use accelerator mode instead of loop devices.
//
// This runs through the server rather than locally because the toolchain image
// lives in the cluster registry, and reaching it needs an identity the CLI does
// not hold. The coordinator builds the image if it is missing, then hands the
// work to the node, which is where the module has to be compiled anyway.
func DiskAcceleratorInstall(ctx *Context, opts struct {
	ConfigCentric

	Force bool   `short:"f" long:"force" description:"Rebuild even when the module is already current"`
	Node  string `position:"0" usage:"Runner to install on (name, ID, or short ID)" required:"true"`
}) error {
	client, err := ctx.RPCClient(rpc.ServiceRunner)
	if err != nil {
		return err
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)

	ctx.Begin("Installing the lbd kernel module on %s", opts.Node)

	res, err := rc.InstallDiskAccelerator(ctx, opts.Node, opts.Force)
	if err != nil {
		return err
	}
	if res.Error() != "" {
		return fmt.Errorf("%s", res.Error())
	}

	ctx.Completed("Accelerator mode is ready on %s, kernel %s", res.Name(), res.KernelRelease())
	ctx.Info("Restart that node's miren service to pick it up")
	return nil
}
