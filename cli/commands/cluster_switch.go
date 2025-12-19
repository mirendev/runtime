package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/ui"
)

func ClusterSwitch(ctx *Context, opts struct {
	ConfigCentric
	Cluster string `position:"0" usage:"Name of the cluster to switch to"`
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

// checkClusterAccess verifies that the user has RBAC permission to access the specified cluster
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

	// First, get the cluster's XID by fetching all clusters and finding a match
	clusters, err := fetchAvailableClusters(ctx, cfg, identity)
	if err != nil {
		// If we can't reach the cloud API, we'll allow the switch
		// The user will get an access denied error when they try to perform actions
		ctx.Warn("Could not verify cluster access: %v", err)
		ctx.Warn("Proceeding with switch. You may encounter permission errors if you don't have access.")
		return nil
	}

	// Find the cluster XID
	var clusterXID string
	for _, c := range clusters {
		if c.Name == clusterName || c.XID == clusterName {
			clusterXID = c.XID
			break
		}
	}

	if clusterXID == "" {
		return fmt.Errorf("cluster %q not found in organization", clusterName)
	}

	// Check RBAC permission via the check-access endpoint
	hasAccess, reason, err := checkClusterAccessRBAC(ctx, cfg, identity, clusterXID)
	if err != nil {
		// If we can't reach the cloud API, we'll allow the switch
		ctx.Warn("Could not verify cluster access: %v", err)
		ctx.Warn("Proceeding with switch. You may encounter permission errors if you don't have access.")
		return nil
	}

	if !hasAccess {
		if reason != "" {
			return fmt.Errorf("access denied to cluster %q: %s", clusterName, reason)
		}
		return fmt.Errorf("access denied to cluster %q: you do not have RBAC permission to access this cluster", clusterName)
	}

	return nil
}

// checkClusterAccessRBAC calls the cloud API to check if the user has RBAC permission to access a cluster
func checkClusterAccessRBAC(ctx *Context, config *clientconfig.Config, identity *clientconfig.IdentityConfig, clusterXID string) (bool, string, error) {
	if identity.Type != "keypair" {
		return false, "", fmt.Errorf("RBAC check is only supported for keypair identities")
	}

	// Get the issuer URL
	issuerURL := identity.Issuer
	if issuerURL == "" {
		return false, "", fmt.Errorf("identity has no issuer configured")
	}

	// Get the private key (handles both direct PrivateKey and KeyRef)
	privateKeyPEM, err := config.GetPrivateKeyPEM(identity)
	if err != nil {
		return false, "", fmt.Errorf("failed to get private key: %w", err)
	}

	// Load the private key
	keyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
	if err != nil {
		return false, "", fmt.Errorf("failed to load private key: %w", err)
	}

	// Get JWT token
	token, err := clientconfig.AuthenticateWithKey(ctx, issuerURL, keyPair)
	if err != nil {
		return false, "", fmt.Errorf("failed to authenticate: %w", err)
	}

	// Make request to check cluster access
	checkURL, err := url.JoinPath(issuerURL, "/api/v1/clusters/", clusterXID, "/check-access")
	if err != nil {
		return false, "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("failed to check cluster access: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var response struct {
		HasAccess          bool   `json:"has_access"`
		RequiredPermission string `json:"required_permission"`
		Reason             string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return false, "", fmt.Errorf("failed to parse response: %w", err)
	}

	return response.HasAccess, response.Reason, nil
}
