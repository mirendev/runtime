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

		selected, err := ui.RunPicker(items,
			ui.WithTitle("Select a cluster to remove:"),
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

	wasActive := cfg.ActiveCluster() == clusterName

	// Remove the cluster
	err = cfg.RemoveCluster(clusterName)
	if err != nil {
		return fmt.Errorf("failed to remove cluster: %w", err)
	}

	// Removing the active cluster leaves none active, and a command run in
	// that state can't do anything useful. Resolve it here rather than sending
	// the user off to another command.
	//
	// With one cluster left there's no ambiguity about what they meant, so take
	// it. With several, ask, because silently pointing someone at a cluster they
	// didn't choose is how you end up reading production while believing you're
	// looking at staging.
	var promoted string
	if wasActive {
		remaining := cfg.GetClusterNames()

		switch {
		case len(remaining) == 1:
			promoted = remaining[0]
		case len(remaining) > 1 && ui.IsInteractive():
			items := make([]ui.PickerItem, len(remaining))
			for i, name := range remaining {
				items[i] = ui.SimplePickerItem{Text: name}
			}

			selected, err := ui.RunPicker(items,
				ui.WithTitle(fmt.Sprintf("Removed %q, which was active. Select the new active cluster:", clusterName)),
				ui.WithHeaders([]string{"CLUSTER"}),
			)
			// A picker that won't run, or that the user escapes out of, is not a
			// failure to remove the cluster. That already happened; fall through
			// to the advice below.
			if err == nil && selected != nil {
				promoted = selected.ID()
			}
		}

		if promoted != "" {
			if err := cfg.SetActiveCluster(promoted); err != nil {
				return fmt.Errorf("failed to select remaining cluster: %w", err)
			}
		}
	}

	// Save the configuration
	err = cfg.Save()
	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	ctx.Printf("Removed cluster: %s\n", clusterName)

	switch {
	case promoted != "":
		ctx.Printf("Active cluster is now: %s\n", promoted)
	case wasActive && cfg.GetClusterCount() > 0:
		ctx.Printf("\nThat was the active cluster, so no cluster is active now.\n")
		ctx.Printf("Pick one with: miren cluster switch <name>\n")
	case wasActive:
		ctx.Printf("\nNo clusters are configured now. Add one with: miren cluster add\n")
	}

	return nil
}
