package commands

import (
	"fmt"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func RunnerRemove(ctx *Context, opts struct {
	ConfigCentric

	Yes   bool   `short:"y" long:"yes" description:"Skip confirmation prompt"`
	Force bool   `long:"force" short:"f" description:"Force removal even if the runner has active sandboxes"`
	Node  string `position:"0" usage:"Runner to remove (name, ID, or short ID)" required:"true"`
}) error {
	client, err := ctx.RPCClient(rpc.ServiceRunner)
	if err != nil {
		return err
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)

	confirmed, err := confirmDeletion(fmt.Sprintf("Delete runner %q and its associated resources?", opts.Node), opts.Yes, false, true)
	if err != nil {
		return err
	}
	if !confirmed {
		ctx.Printf("Deletion cancelled\n")
		return nil
	}

	res, err := rc.RemoveRunner(ctx, opts.Node, opts.Force)
	if err != nil {
		return err
	}

	if res.Error() != "" {
		return fmt.Errorf("%s", res.Error())
	}

	ctx.Printf("Removed runner %q", res.Name())
	if res.RemovedResources() > 0 {
		ctx.Printf(" (%d associated resources cleaned up)", res.RemovedResources())
	}
	ctx.Printf("\n")

	return nil
}
