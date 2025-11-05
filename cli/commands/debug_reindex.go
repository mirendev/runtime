package commands

import (
	"fmt"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

// DebugReindex rebuilds all entity indexes from scratch
func DebugReindex(ctx *Context, opts struct {
	ConfigCentric
	DryRun  bool `short:"d" long:"dry-run" description:"Show what would be done without making changes"`
	Confirm bool `short:"y" long:"yes" description:"Skip confirmation prompt"`
}) error {

	if !opts.DryRun && !opts.Confirm {
		ctx.Warn("⚠️  This will rebuild ALL entity indexes by:")
		ctx.Warn("   1. Rebuilding indexes for all existing entities")
		ctx.Warn("   2. Cleaning up stale index entries")
		ctx.Warn("")
		ctx.Warn("This operation is safe and can run during normal operation.")
		ctx.Warn("It may take several minutes depending on the number of entities.")
		ctx.Warn("")

		confirmed, err := ui.Confirm(
			ui.WithMessage("Continue with reindex?"),
			ui.WithDefault(false),
		)
		if err != nil {
			return fmt.Errorf("confirmation failed: %w", err)
		}
		if !confirmed {
			return fmt.Errorf("cancelled")
		}
	}

	// Connect to entity server
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	if opts.DryRun {
		ctx.Info("🔍 Running in dry-run mode (no changes will be made)")
	} else {
		ctx.Info("🔄 Starting reindex operation...")
	}

	// Call reindex RPC
	resp, err := eac.Reindex(ctx, opts.DryRun)
	if err != nil {
		return fmt.Errorf("reindex failed: %w", err)
	}

	// Convert stats list to map for easy access
	statsMap := make(map[string]int64)
	if resp.HasStats() {
		for _, stat := range resp.Stats() {
			if stat.HasName() && stat.HasValue() {
				statsMap[stat.Name()] = stat.Value()
			}
		}
	}

	// Display results
	ctx.Info("")
	ctx.Info("✅ Reindex complete!")
	ctx.Info("")
	ctx.Info("Work completed:")
	ctx.Info("  • Entities processed: %d", statsMap["entities_processed"])
	ctx.Info("  • Indexes rebuilt: %d", statsMap["indexes_rebuilt"])
	ctx.Info("")
	ctx.Info("Health check:")
	ctx.Info("  • Collection entries scanned: %d", statsMap["collection_entries_scanned"])
	ctx.Info("  • Stale entries found: %d", statsMap["stale_entries_found"])
	if !opts.DryRun {
		ctx.Info("  • Stale entries removed: %d", statsMap["stale_entries_removed"])
	}

	return nil
}
