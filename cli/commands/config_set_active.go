package commands

import (
	"fmt"
)

func ConfigSetActive(ctx *Context, opts struct {
	Cluster string `position:"0" usage:"Name of the cluster to set as active"`
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

		// Use the shared cluster selection
		selected, err := SelectCluster(ctx, "Select a cluster to set as active:", clusterNames, cfg.ActiveCluster(), false)
		if err != nil {
			// If we can't run interactive mode (no TTY), show available clusters
			ctx.Printf("Cannot run interactive mode. Available clusters:\n")
			for _, name := range clusterNames {
				prefix := "  "
				if name == cfg.ActiveCluster() {
					prefix = "* "
				}
				ctx.Printf("%s%s\n", prefix, name)
			}
			ctx.Printf("\nUsage: runtime config set-active <cluster-name>\n")
			return nil
		}

		if selected == "" {
			// User cancelled
			return nil
		}

		clusterName = selected
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

	ctx.Printf("Active cluster set to: %s\n", clusterName)
	return nil
}
