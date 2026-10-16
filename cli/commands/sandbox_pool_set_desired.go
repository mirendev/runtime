package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/ui"
)

func SandboxPoolSetDesired(ctx *Context, opts struct {
	RawID   bool   `long:"raw-id" description:"Use the provided ID as-is without adding the pool/ prefix"`
	PoolID  string `position:"0" usage:"Pool ID (e.g., pool-CUSkT8J58BmgkDeGyPP2e or pool/pool-CUSkT8J58BmgkDeGyPP2e)" required:"true"`
	Desired string `position:"1" usage:"Desired instance count (absolute number, +N to increase, or -N to decrease)" required:"true"`
	ConfigCentric
}) error {
	desiredStr := strings.TrimSpace(opts.Desired)
	count, err := strconv.ParseInt(desiredStr, 10, 64)
	if err != nil {
		if strings.HasPrefix(desiredStr, "+") || strings.HasPrefix(desiredStr, "-") {
			return fmt.Errorf("invalid relative count %q: %v", opts.Desired, err)
		}
		return fmt.Errorf("invalid count %q: %v", opts.Desired, err)
	}

	client, err := ctx.RPCClient("dev.miren.runtime/sandbox-pools")
	if err != nil {
		return err
	}
	defer client.Close()
	poolID := opts.PoolID
	if !opts.RawID && !strings.HasPrefix(poolID, "pool/") {
		poolID = "pool/" + poolID
	}
	relative := strings.HasPrefix(desiredStr, "+") || strings.HasPrefix(desiredStr, "-")
	res, err := compute_v1alpha.NewSandboxPoolsClient(client).SetDesired(ctx, poolID, count, relative, 0, opts.RawID)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return fmt.Errorf("sandbox pool not found: %s (error: %w)", opts.PoolID, err)
		}
		return err
	}
	poolDisplay := ui.DisplayShortID(res.PoolShortId(), res.PoolId())
	if !res.Changed() {
		ctx.Printf("Pool %s (service=%s) already has desired_instances=%d\n",
			poolDisplay, res.Service(), res.Desired())
		return nil
	}

	ctx.Printf("Successfully updated sandbox pool:\n")
	ctx.Printf("  Pool ID: %s\n", poolDisplay)
	ctx.Printf("  Service: %s\n", res.Service())
	ctx.Printf("  Previous desired_instances: %d\n", res.PreviousDesired())
	ctx.Printf("  New desired_instances: %d\n", res.Desired())
	ctx.Printf("  Current instances: %d\n", res.CurrentInstances())
	ctx.Printf("  Ready instances: %d\n", res.ReadyInstances())
	ctx.Printf("  Revision: %d\n", res.Revision())

	return nil
}
