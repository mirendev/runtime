package commands

import (
	"fmt"

	"miren.dev/runtime/pkg/ui"
)

func ClusterSwitch(ctx *Context, opts struct {
	ConfigCentric
	Args struct {
		Cluster string `positional-arg-name:"cluster" description:"Name of the cluster to switch to"`
	} `positional-args:"yes"`
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	clusterName := opts.Args.Cluster

	// If no cluster name provided, show interactive menu
	if clusterName == "" {
		// Get sorted list of cluster names
		clusterNames := cfg.GetClusterNames()

		if len(clusterNames) == 0 {
			ctx.Printf("No clusters configured\n")
			ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
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

		// Run the picker with single column
		selected, err := ui.RunPicker(items,
			ui.WithTitle("Select a cluster to switch to:"),
			ui.WithHeaders([]string{"CLUSTER"}),
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
			ctx.Printf("\nUsage: miren cluster switch <cluster-name>\n")
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

	// Set the active cluster
	err = cfg.SetActiveCluster(clusterName)
	if err != nil {
		return fmt.Errorf("failed to set active cluster: %w", err)
	}

	// Save the configuration
	err = cfg.Save()
	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	ctx.Printf("Switched to cluster: %s\n", clusterName)
	return nil
}
