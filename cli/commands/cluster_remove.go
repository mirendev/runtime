package commands

import (
	"fmt"

	"miren.dev/runtime/pkg/ui"
)

func ClusterRemove(ctx *Context, opts struct {
	Cluster string `position:"0" usage:"Name of the cluster to remove"`
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	clusterName := opts.Cluster

	// If no cluster name provided, show interactive menu
	if clusterName == "" {
		// Get sorted list of cluster names
		clusterNames := cfg.GetClusterNames()

		if len(clusterNames) == 0 {
			ctx.Printf("No clusters configured\n")
			return nil
		}

		// Create picker items
		items := make([]ui.PickerItem, len(clusterNames))
		activeCluster := cfg.ActiveCluster()
		for i, name := range clusterNames {
			items[i] = ui.SimplePickerItem{
				Text:   name,
				Active: name == activeCluster,
			}
		}

		// Run the picker with disabled check for active cluster
		selected, err := ui.RunPicker(items,
			ui.WithTitle("Select a cluster to remove:"),
			ui.WithHeaders([]string{"CLUSTER"}),
			ui.WithDisabledCheck(func(item ui.PickerItem) bool {
				return item.ID() == activeCluster
			}, "Cannot remove the active cluster"),
			ui.WithFooter("Note: You cannot remove the active cluster"),
		)

		if err != nil {
			// If we can't run interactive mode (no TTY), show available clusters
			ctx.Printf("Cannot run interactive mode. Available clusters:\n")
			for _, name := range clusterNames {
				prefix := "  "
				if name == activeCluster {
					prefix = "* "
				}
				ctx.Printf("%s%s\n", prefix, name)
			}
			ctx.Printf("\nUsage: miren cluster remove <cluster-name>\n")
			return nil
		}

		if selected == nil {
			// User cancelled
			return nil
		}

		clusterName = selected.ID()
	}

	// Check if the cluster exists
	if !cfg.HasCluster(clusterName) {
		availableClusters := cfg.GetClusterNames()
		return fmt.Errorf("cluster %q not found. Available clusters: %v", clusterName, availableClusters)
	}

	// Check if this is the active cluster
	if cfg.ActiveCluster() == clusterName {
		return fmt.Errorf("cannot remove active cluster %q. Please switch to another cluster first using 'miren cluster switch'", clusterName)
	}

	// Remove the cluster
	err = cfg.RemoveCluster(clusterName)
	if err != nil {
		return fmt.Errorf("failed to remove cluster: %w", err)
	}

	// Save the configuration
	err = cfg.Save()
	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	ctx.Printf("Removed cluster: %s\n", clusterName)
	return nil
}
