package commands

import (
	"fmt"

	"miren.dev/runtime/clientconfig"
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

	// Check if the user has permission to access this cluster
	if err := checkClusterAccess(ctx, cfg, clusterName); err != nil {
		return fmt.Errorf("access denied to cluster %q: %w", clusterName, err)
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

// checkClusterAccess verifies that the user has permission to access the specified cluster
func checkClusterAccess(ctx *Context, cfg *clientconfig.Config, clusterName string) error {
	// Get the cluster configuration
	cluster, err := cfg.GetCluster(clusterName)
	if err != nil {
		return fmt.Errorf("failed to get cluster config: %w", err)
	}

	// Skip permission check if no identity is configured (e.g., local dev clusters)
	if cluster.Identity == "" {
		return nil
	}

	// Get the identity for this cluster
	identity, err := cfg.GetIdentity(cluster.Identity)
	if err != nil {
		return fmt.Errorf("identity %q not found: %w", cluster.Identity, err)
	}

	// Fetch accessible clusters from the cloud API
	accessibleClusters, err := fetchAvailableClusters(ctx, identity)
	if err != nil {
		// If we can't reach the cloud API, we'll allow the switch
		// The user will get an access denied error when they try to perform actions
		ctx.Warn("Could not verify cluster access: %v", err)
		ctx.Warn("Proceeding with switch. You may encounter permission errors if you don't have access.")
		return nil
	}

	// Check if the target cluster is in the accessible list
	for _, accessibleCluster := range accessibleClusters {
		// Match by either name or XID (cluster name in local config may match either)
		if accessibleCluster.Name == clusterName || accessibleCluster.XID == clusterName {
			return nil // Access granted
		}
	}

	// Cluster not found in accessible list
	return fmt.Errorf("you do not have permission to access this cluster")
}
