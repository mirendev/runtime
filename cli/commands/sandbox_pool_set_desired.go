package commands

import (
	"fmt"
	"strconv"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func SandboxPoolSetDesired(ctx *Context, opts struct {
	RawID   bool   `long:"raw-id" description:"Use the provided ID as-is without adding the pool/ prefix"`
	PoolID  string `position:"0" usage:"Pool ID (e.g., pool-CUSkT8J58BmgkDeGyPP2e or pool/pool-CUSkT8J58BmgkDeGyPP2e)" required:"true"`
	Desired string `position:"1" usage:"Desired instance count (absolute number, +N to increase, or -N to decrease)" required:"true"`
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Normalize the pool ID - accept both "pool-ABC" and "pool/pool-ABC" formats
	// Add the "pool/" prefix if it's not already present (unless --raw-id is used)
	poolID := opts.PoolID
	if !opts.RawID && !strings.HasPrefix(poolID, "pool/") {
		poolID = "pool/" + poolID
	}

	// Look up the pool by ID
	var pool compute_v1alpha.SandboxPool
	getRes, err := eac.Get(ctx, poolID)
	if err != nil {
		return fmt.Errorf("sandbox pool not found: %s (error: %v)", opts.PoolID, err)
	}

	pool.Decode(getRes.Entity().Entity())

	// Parse the desired count (absolute or relative)
	var newDesired int64
	desiredStr := strings.TrimSpace(opts.Desired)

	if strings.HasPrefix(desiredStr, "+") || strings.HasPrefix(desiredStr, "-") {
		// Relative adjustment
		delta, err := strconv.ParseInt(desiredStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid relative count %q: %v", opts.Desired, err)
		}
		newDesired = pool.DesiredInstances + delta
		if newDesired < 0 {
			return fmt.Errorf("relative adjustment would result in negative count: %d + %d = %d", pool.DesiredInstances, delta, newDesired)
		}
	} else {
		// Absolute count
		count, err := strconv.ParseInt(desiredStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid count %q: %v", opts.Desired, err)
		}
		if count < 0 {
			return fmt.Errorf("count cannot be negative: %d", count)
		}
		newDesired = count
	}

	// Check if there's actually a change
	if newDesired == pool.DesiredInstances {
		ctx.Printf("Pool %s (service=%s) already has desired_instances=%d\n",
			pool.ID.String(), pool.Service, pool.DesiredInstances)
		return nil
	}

	// Prepare the patch
	attrs := []entity.Attr{
		{
			ID:    entity.DBId,
			Value: entity.AnyValue(pool.ID),
		},
		{
			ID:    compute_v1alpha.SandboxPoolDesiredInstancesId,
			Value: entity.AnyValue(newDesired),
		},
	}

	// Apply the patch with optimistic concurrency control
	patchRes, err := eac.Patch(ctx, attrs, getRes.Entity().Revision())
	if err != nil {
		return fmt.Errorf("failed to patch sandbox pool: %v", err)
	}

	ctx.Printf("Successfully updated sandbox pool:\n")
	ctx.Printf("  Pool ID: %s\n", pool.ID.String())
	ctx.Printf("  Service: %s\n", pool.Service)
	ctx.Printf("  Previous desired_instances: %d\n", pool.DesiredInstances)
	ctx.Printf("  New desired_instances: %d\n", newDesired)
	ctx.Printf("  Current instances: %d\n", pool.CurrentInstances)
	ctx.Printf("  Ready instances: %d\n", pool.ReadyInstances)
	ctx.Printf("  Revision: %d\n", patchRes.Revision())

	return nil
}
