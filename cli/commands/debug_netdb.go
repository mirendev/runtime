package commands

import (
	"fmt"
	"net/netip"
	"slices"

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

	// Sort by IP address numerically
	slices.SortFunc(leases, func(a, b *debug_v1alpha.IPLease) int {
		addrA, _ := netip.ParseAddr(a.Ip())
		addrB, _ := netip.ParseAddr(b.Ip())
		return addrA.Compare(addrB)
	})

	headers := []string{"IP", "SUBNET", "STATUS", "SANDBOX", "RELEASED"}
	rows := make([]ui.Row, len(leases))

	for i, lease := range leases {
		status := "released"
		if lease.Reserved() {
			status = "reserved"
		}

		sandboxID := "-"
		if lease.HasSandboxId() {
			sandboxID = lease.SandboxId()
		}

		releasedStr := "-"
		if lease.HasReleasedAt() {
			releasedStr = humanFriendlyTimestamp(standard.FromTimestamp(lease.ReleasedAt()))
		}

		rows[i] = ui.Row{lease.Ip(), lease.Subnet(), status, sandboxID, releasedStr}
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0, 1, 3))
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
	Force  bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/debug-netdb")
	if err != nil {
		return err
	}

	netdb := debug_v1alpha.NewNetDBClient(client)

	// First, do a dry run to find orphaned IPs
	results, err := netdb.Gc(ctx, opts.Subnet, true)
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
		return nil
	}

	// Confirm before releasing
	if !opts.Force {
		confirmed, err := ui.Confirm(
			ui.WithMessage(fmt.Sprintf("Release %d orphaned IP lease(s)?", len(orphanedIPs))),
			ui.WithDefault(false),
		)
		if err != nil {
			return fmt.Errorf("confirmation failed: %w", err)
		}
		if !confirmed {
			ctx.Info("GC cancelled")
			return nil
		}
	}

	// Actually release the IPs
	results, err = netdb.Gc(ctx, opts.Subnet, false)
	if err != nil {
		return fmt.Errorf("failed to release orphaned IPs: %w", err)
	}

	ctx.Completed("Released %d orphaned IP leases", results.ReleasedCount())
	return nil
}
