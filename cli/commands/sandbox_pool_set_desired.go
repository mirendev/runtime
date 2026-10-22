package commands

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
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
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(ctx, client, "setDesired")
	}
	if err != nil && !isMissingRPC(err) {
		return err
	}
	poolID := opts.PoolID
	if !opts.RawID && !strings.HasPrefix(poolID, "pool/") {
		poolID = "pool/" + poolID
	}
	relative := strings.HasPrefix(desiredStr, "+") || strings.HasPrefix(desiredStr, "-")
	var res *compute_v1alpha.SandboxPoolsClientSetDesiredResults
	if err == nil {
		res, err = compute_v1alpha.NewSandboxPoolsClient(client).SetDesired(ctx, poolID, count, relative, 0, opts.RawID)
	}
	if isMissingRPC(err) {
		return sandboxPoolSetDesiredLegacy(ctx, poolID, count, relative)
	}
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

func sandboxPoolSetDesiredLegacy(ctx *Context, poolID string, count int64, relative bool) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer client.Close()
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	res, err := eac.Get(ctx, poolID)
	if err != nil {
		return fmt.Errorf("sandbox pool not found: %s (error: %w)", poolID, err)
	}
	var pool compute_v1alpha.SandboxPool
	pool.Decode(res.Entity().Entity())
	if relative {
		if (count > 0 && pool.DesiredInstances > math.MaxInt64-count) || (count < 0 && pool.DesiredInstances < math.MinInt64-count) {
			return fmt.Errorf("relative adjustment overflows instance count")
		}
		if pool.DesiredInstances+count < 0 {
			return fmt.Errorf("relative adjustment would result in negative count: %d + %d = %d", pool.DesiredInstances, count, pool.DesiredInstances+count)
		}
		count += pool.DesiredInstances
	}
	if count < 0 {
		return fmt.Errorf("count cannot be negative: %d", count)
	}
	if count > concurrency.MaxPoolSize {
		return fmt.Errorf("count exceeds maximum pool size %d: %d", concurrency.MaxPoolSize, count)
	}
	poolDisplay := ui.BriefId(res.Entity().Entity())
	if count == pool.DesiredInstances {
		ctx.Printf("Pool %s (service=%s) already has desired_instances=%d\n", poolDisplay, pool.Service, count)
		return nil
	}
	patch, err := eac.Patch(ctx, []entity.Attr{entity.Ref(entity.DBId, pool.ID), entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, count)}, res.Entity().Revision())
	if err != nil {
		return fmt.Errorf("failed to patch sandbox pool: %w", err)
	}
	ctx.Printf("Successfully updated sandbox pool:\n")
	ctx.Printf("  Pool ID: %s\n", poolDisplay)
	ctx.Printf("  Service: %s\n", pool.Service)
	ctx.Printf("  Previous desired_instances: %d\n", pool.DesiredInstances)
	ctx.Printf("  New desired_instances: %d\n", count)
	ctx.Printf("  Current instances: %d\n", pool.CurrentInstances)
	ctx.Printf("  Ready instances: %d\n", pool.ReadyInstances)
	ctx.Printf("  Revision: %d\n", patch.Revision())
	return nil
}
