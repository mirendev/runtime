package commands

import (
	"fmt"
)

func ConfigRemove(ctx *Context, opts struct {
	ConfigCentric
	Force bool `short:"f" long:"force" description:"Force removal without confirmation"`
	Args  struct {
		Cluster string `positional-arg-name:"cluster" description:"Name of the cluster to remove"`
	} `positional-args:"yes"`
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	// Check if there's only one cluster
	if cfg.GetClusterCount() <= 1 {
		return fmt.Errorf("cannot remove the last cluster")
	}

	clusterName := opts.Args.Cluster

	// If no cluster name provided, show interactive menu
	if clusterName == "" {
		// Get sorted list of cluster names
		clusterNames := cfg.GetClusterNames()

		// Use the shared cluster selection with dimming for active cluster
		selected, err := SelectCluster(ctx, "Select a cluster to remove:", clusterNames, cfg.ActiveCluster(), true)
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
			ctx.Printf("\nUsage: runtime config remove <cluster-name>\n")
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

	// Check if trying to remove the active cluster
	if clusterName == cfg.ActiveCluster() {
		return fmt.Errorf("cannot remove the active cluster %q. Please switch to another cluster first", clusterName)
	}

	// Ask for confirmation unless --force is used
	if !opts.Force {
		ctx.Printf("This will remove cluster '%s' from your configuration.\n", clusterName)
		ctx.Printf("To confirm, run the command with --force flag\n")
		return nil
	}

	// Remove the cluster
	err = cfg.RemoveCluster(clusterName)
	if err != nil {
		return err
	}

	// Save the configuration
	err = cfg.Save()
	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	ctx.Printf("Removed cluster: %s\n", clusterName)
	return nil
}
