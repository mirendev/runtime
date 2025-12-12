package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/ui"
)

// DebugNetDBList lists all IP leases from the netdb
func DebugNetDBList(ctx *Context, opts struct {
	ConfigCentric
	Subnet   string `short:"s" long:"subnet" description:"Filter by subnet CIDR"`
	Reserved bool   `short:"r" long:"reserved" description:"Show only reserved (in-use) IPs"`
	Released bool   `short:"R" long:"released" description:"Show only released IPs"`
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/debug-netdb")
	if err != nil {
		return err
	}

	netdb := debug_v1alpha.NewNetDBClient(client)

	results, err := netdb.ListLeases(ctx, opts.Subnet, opts.Reserved, opts.Released)
	if err != nil {
		return fmt.Errorf("failed to list leases: %w", err)
	}

	leases := results.Leases()
	if len(leases) == 0 {
		ctx.Info("No IP leases found")
		return nil
	}

	headers := []string{"IP", "SUBNET", "STATUS", "RELEASED_AT"}
	rows := make([]ui.Row, len(leases))

	for i, lease := range leases {
		status := "released"
		if lease.Reserved() {
			status = "reserved"
		}

		releasedStr := "-"
		if lease.HasReleasedAt() {
			releasedStr = standard.FromTimestamp(lease.ReleasedAt()).Format(time.RFC3339)
		}

		rows[i] = ui.Row{lease.Ip(), lease.Subnet(), status, releasedStr}
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0, 1))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	ctx.Info("Total: %d leases", len(leases))

	return nil
}

// DebugNetDBStatus shows subnet utilization stats
func DebugNetDBStatus(ctx *Context, opts struct {
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/debug-netdb")
	if err != nil {
		return err
	}

	netdb := debug_v1alpha.NewNetDBClient(client)

	results, err := netdb.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	subnets := results.Subnets()
	if len(subnets) == 0 {
		ctx.Info("No subnets found")
		return nil
	}

	headers := []string{"SUBNET", "CAPACITY", "RESERVED", "RELEASED", "USAGE"}
	rows := make([]ui.Row, len(subnets))

	for i, subnet := range subnets {
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

		rows[i] = ui.Row{
			subnet.Subnet(),
			fmt.Sprintf("%d", capacity),
			fmt.Sprintf("%d", reserved),
			fmt.Sprintf("%d", subnet.Released()),
			usageStr,
		}
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

// DebugNetDBRelease manually releases IP leases
func DebugNetDBRelease(ctx *Context, opts struct {
	ConfigCentric
	IP     string `short:"i" long:"ip" description:"Specific IP to release"`
	Subnet string `short:"s" long:"subnet" description:"Release all reserved IPs in subnet"`
	All    bool   `short:"a" long:"all" description:"Release all reserved IPs (use with caution)"`
	Force  bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
}) error {
	if opts.IP == "" && opts.Subnet == "" && !opts.All {
		return fmt.Errorf("must specify --ip, --subnet, or --all")
	}

	client, err := ctx.RPCClient("dev.miren.runtime/debug-netdb")
	if err != nil {
		return err
	}

	netdb := debug_v1alpha.NewNetDBClient(client)

	if opts.IP != "" {
		if !opts.Force {
			confirmed, err := ui.Confirm(
				ui.WithMessage(fmt.Sprintf("Release IP %s? This will make it available for reallocation.", opts.IP)),
				ui.WithDefault(false),
			)
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !confirmed {
				ctx.Info("Release cancelled")
				return nil
			}
		}

		result, err := netdb.ReleaseIP(ctx, opts.IP)
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
			confirmed, err := ui.Confirm(
				ui.WithMessage(fmt.Sprintf("Release all reserved IPs in subnet %s?", opts.Subnet)),
				ui.WithDefault(false),
			)
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !confirmed {
				ctx.Info("Release cancelled")
				return nil
			}
		}

		result, err := netdb.ReleaseSubnet(ctx, opts.Subnet)
		if err != nil {
			return fmt.Errorf("failed to release subnet IPs: %w", err)
		}

		ctx.Completed("Released %d IP lease(s) in subnet %s", result.Count(), opts.Subnet)
		return nil
	}

	if opts.All {
		if !opts.Force {
			confirmed, err := ui.Confirm(
				ui.WithMessage("Release ALL reserved IPs across all subnets?"),
				ui.WithDefault(false),
			)
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !confirmed {
				ctx.Info("Release cancelled")
				return nil
			}
		}

		result, err := netdb.ReleaseAll(ctx)
		if err != nil {
			return fmt.Errorf("failed to release all IPs: %w", err)
		}

		ctx.Completed("Released %d IP lease(s)", result.Count())
		return nil
	}

	return nil
}

// DebugNetDBGC garbage collects IPs not associated with live sandboxes
func DebugNetDBGC(ctx *Context, opts struct {
	ConfigCentric
	Subnet string `short:"s" long:"subnet" description:"Only GC IPs in this subnet"`
	DryRun bool   `short:"n" long:"dry-run" description:"Show what would be released without making changes"`
	Force  bool   `short:"f" long:"force" description:"Actually perform the GC (required unless --dry-run)"`
}) error {
	if !opts.DryRun && !opts.Force {
		ctx.Warn("Dry run mode (use --force to actually release IPs)")
		opts.DryRun = true
	}

	client, err := ctx.RPCClient("dev.miren.runtime/debug-netdb")
	if err != nil {
		return err
	}

	netdb := debug_v1alpha.NewNetDBClient(client)

	results, err := netdb.Gc(ctx, opts.Subnet, opts.DryRun)
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
