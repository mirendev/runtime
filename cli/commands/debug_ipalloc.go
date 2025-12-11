package commands

import (
	"fmt"
	"strings"
	"time"

	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

// DebugIPAllocList lists all IP leases from the netdb
func DebugIPAllocList(ctx *Context, opts struct {
	ConfigCentric
	Subnet   string `short:"s" long:"subnet" description:"Filter by subnet CIDR"`
	Reserved bool   `short:"r" long:"reserved" description:"Show only reserved (in-use) IPs"`
	Released bool   `short:"R" long:"released" description:"Show only released IPs"`
	Stuck    bool   `short:"S" long:"stuck" description:"Show only stuck IPs (reserved=1, no released_at)"`
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/debug-ipalloc")
	if err != nil {
		return err
	}

	ipalloc := debug_v1alpha.NewIPAllocClient(client)

	results, err := ipalloc.ListLeases(ctx, opts.Subnet, opts.Reserved, opts.Released, opts.Stuck)
	if err != nil {
		return fmt.Errorf("failed to list leases: %w", err)
	}

	ctx.Info("IP Leases:")
	ctx.Info("")
	ctx.Info("%-18s %-18s %-10s %s", "IP", "SUBNET", "STATUS", "RELEASED_AT")
	ctx.Info("%-18s %-18s %-10s %s", strings.Repeat("-", 18), strings.Repeat("-", 18), strings.Repeat("-", 10), strings.Repeat("-", 20))

	leases := results.Leases()
	for _, lease := range leases {
		status := "released"
		if lease.Reserved() {
			status = "reserved"
		}

		releasedStr := "-"
		if lease.HasReleasedAt() {
			releasedStr = standard.FromTimestamp(lease.ReleasedAt()).Format(time.RFC3339)
		}

		ctx.Info("%-18s %-18s %-10s %s", lease.Ip(), lease.Subnet(), status, releasedStr)
	}

	ctx.Info("")
	ctx.Info("Total: %d leases", len(leases))

	return nil
}

// DebugIPAllocStatus shows subnet utilization stats
func DebugIPAllocStatus(ctx *Context, opts struct {
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/debug-ipalloc")
	if err != nil {
		return err
	}

	ipalloc := debug_v1alpha.NewIPAllocClient(client)

	results, err := ipalloc.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	ctx.Info("IP Allocation Status:")
	ctx.Info("")
	ctx.Info("%-20s %8s %10s %10s %8s %8s", "SUBNET", "CAPACITY", "RESERVED", "RELEASED", "STUCK", "USAGE")
	ctx.Info("%-20s %8s %10s %10s %8s %8s", strings.Repeat("-", 20), strings.Repeat("-", 8), strings.Repeat("-", 10), strings.Repeat("-", 10), strings.Repeat("-", 8), strings.Repeat("-", 8))

	for _, subnet := range results.Subnets() {
		capacity := subnet.Capacity()
		reserved := subnet.Reserved()

		usage := float64(0)
		if capacity > 0 {
			usage = float64(reserved) / float64(capacity) * 100
		}
		usageStr := fmt.Sprintf("%.1f%%", usage)
		if usage > 90 {
			usageStr += " ⚠️"
		}

		ctx.Info("%-20s %8d %10d %10d %8d %8s", subnet.Subnet(), capacity, reserved, subnet.Released(), subnet.Stuck(), usageStr)
	}

	return nil
}

// DebugIPAllocRelease manually releases IP leases
func DebugIPAllocRelease(ctx *Context, opts struct {
	ConfigCentric
	IP     string `short:"i" long:"ip" description:"Specific IP to release"`
	Subnet string `short:"s" long:"subnet" description:"Release all stuck IPs in subnet"`
	All    bool   `short:"a" long:"all" description:"Release all stuck IPs (use with caution)"`
	Force  bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
}) error {
	if opts.IP == "" && opts.Subnet == "" && !opts.All {
		return fmt.Errorf("must specify --ip, --subnet, or --all")
	}

	client, err := ctx.RPCClient("dev.miren.runtime/debug-ipalloc")
	if err != nil {
		return err
	}

	ipalloc := debug_v1alpha.NewIPAllocClient(client)

	if opts.IP != "" {
		if !opts.Force {
			ctx.Warn("About to release IP %s", opts.IP)
			ctx.Warn("This will make this IP available for reallocation.")
			ctx.Warn("Use --force to skip this confirmation.")
			return nil
		}

		result, err := ipalloc.ReleaseIP(ctx, opts.IP)
		if err != nil {
			return fmt.Errorf("failed to release IP: %w", err)
		}

		if result.Released() {
			ctx.Completed("Released IP %s", opts.IP)
		} else {
			ctx.Info("IP %s was not reserved or does not exist", opts.IP)
		}
		return nil
	}

	if opts.Subnet != "" {
		if !opts.Force {
			ctx.Warn("About to release all stuck IPs in subnet %s", opts.Subnet)
			ctx.Warn("This will make these IPs available for reallocation.")
			ctx.Warn("Use --force to skip this confirmation.")
			return nil
		}

		result, err := ipalloc.ReleaseSubnet(ctx, opts.Subnet)
		if err != nil {
			return fmt.Errorf("failed to release subnet IPs: %w", err)
		}

		ctx.Completed("Released %d IP lease(s) in subnet %s", result.Count(), opts.Subnet)
		return nil
	}

	if opts.All {
		if !opts.Force {
			ctx.Warn("About to release ALL stuck IPs across all subnets")
			ctx.Warn("This will make these IPs available for reallocation.")
			ctx.Warn("Use --force to skip this confirmation.")
			return nil
		}

		result, err := ipalloc.ReleaseAll(ctx)
		if err != nil {
			return fmt.Errorf("failed to release all IPs: %w", err)
		}

		ctx.Completed("Released %d IP lease(s)", result.Count())
		return nil
	}

	return nil
}

// DebugIPAllocGC garbage collects IPs not associated with live sandboxes
func DebugIPAllocGC(ctx *Context, opts struct {
	ConfigCentric
	Subnet string `short:"s" long:"subnet" description:"Only GC IPs in this subnet"`
	DryRun bool   `short:"n" long:"dry-run" description:"Show what would be released without making changes"`
	Force  bool   `short:"f" long:"force" description:"Actually perform the GC (required unless --dry-run)"`
}) error {
	if !opts.DryRun && !opts.Force {
		ctx.Warn("Dry run mode (use --force to actually release IPs)")
		opts.DryRun = true
	}

	client, err := ctx.RPCClient("dev.miren.runtime/debug-ipalloc")
	if err != nil {
		return err
	}

	ipalloc := debug_v1alpha.NewIPAllocClient(client)

	results, err := ipalloc.Gc(ctx, opts.Subnet, opts.DryRun)
	if err != nil {
		return fmt.Errorf("failed to run GC: %w", err)
	}

	orphanedIPs := results.OrphanedIps()
	if len(orphanedIPs) == 0 {
		ctx.Info("No orphaned IPs found - all reserved IPs have associated sandboxes")
		return nil
	}

	ctx.Info("Found %d orphaned IPs (reserved but no sandbox):", len(orphanedIPs))
	for _, ip := range orphanedIPs {
		ctx.Info("  %s", ip)
	}

	if opts.DryRun {
		ctx.Info("")
		ctx.Info("Dry run - no changes made. Use --force to release these IPs.")
	} else {
		ctx.Completed("Released %d orphaned IP leases", results.ReleasedCount())
	}

	return nil
}
