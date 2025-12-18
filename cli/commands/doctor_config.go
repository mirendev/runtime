package commands

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/ui"
)

// isLocalCluster checks if a cluster hostname refers to the local machine
func isLocalCluster(hostname string) bool {
	host := hostname
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		host = h
	}
	return isLocalAddress(host)
}

type clusterInfo struct {
	name    string
	cluster *clientconfig.ClusterConfig
	source  string
}

// DoctorConfig shows configuration file information
func DoctorConfig(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
		return err
	}

	ctx.Printf("%s\n", infoBold.Render("Configuration"))
	ctx.Printf("%s\n", infoGray.Render("============="))

	if cfg == nil || errors.Is(err, clientconfig.ErrNoConfig) {
		ctx.Printf("\n%s\n", infoGray.Render("No configuration found"))
		ctx.Printf("\n%s\n", infoLabel.Render("To get started:"))
		ctx.Printf("  %s        %s\n", infoBold.Render("miren login"), infoGray.Render("# Authenticate with miren.cloud"))
		ctx.Printf("  %s  %s\n", infoBold.Render("miren cluster add"), infoGray.Render("# Add a cluster to your config"))
		return nil
	}

	activeCluster := cfg.ActiveCluster()

	// Separate clusters into local and remote
	var localClusters, remoteClusters []clusterInfo
	cfg.IterateClusters(func(name string, cluster *clientconfig.ClusterConfig) error {
		info := clusterInfo{
			name:    name,
			cluster: cluster,
			source:  cfg.GetClusterSource(name),
		}
		if isLocalCluster(cluster.Hostname) {
			localClusters = append(localClusters, info)
		} else {
			remoteClusters = append(remoteClusters, info)
		}
		return nil
	})

	// Shorten paths for display
	homeDir, _ := os.UserHomeDir()
	shortenPath := func(path string) string {
		if homeDir != "" && strings.HasPrefix(path, homeDir) {
			return "~" + path[len(homeDir):]
		}
		return path
	}

	// Display local clusters table
	if len(localClusters) > 0 {
		ctx.Printf("\n%s\n", infoLabel.Render("Local:"))
		headers := []string{"CLUSTER", "ADDRESS", "SOURCE FILE"}
		var rows []ui.Row
		for _, c := range localClusters {
			name := c.name
			if c.name == activeCluster {
				name = infoGreen.Render(c.name + "*")
			}
			rows = append(rows, ui.Row{name, c.cluster.Hostname, shortenPath(c.source)})
		}
		columns := ui.AutoSizeColumns(headers, rows, nil)
		table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
		ctx.Printf("%s\n", table.Render())
	}

	// Display remote clusters table
	if len(remoteClusters) > 0 {
		ctx.Printf("\n%s\n", infoLabel.Render("Remote:"))
		headers := []string{"CLUSTER", "ADDRESS", "SOURCE FILE"}
		var rows []ui.Row
		for _, c := range remoteClusters {
			name := c.name
			if c.name == activeCluster {
				name = infoGreen.Render(c.name + "*")
			}
			rows = append(rows, ui.Row{name, c.cluster.Hostname, shortenPath(c.source)})
		}
		columns := ui.AutoSizeColumns(headers, rows, nil)
		table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
		ctx.Printf("%s\n", table.Render())
	}

	if len(localClusters) > 0 || len(remoteClusters) > 0 {
		ctx.Printf("%s\n", infoGray.Render("* = active cluster"))
	}

	// Check for local server registration
	existing, _ := registration.LoadRegistration("/var/lib/miren/server")

	// Interactive mode
	if !ui.IsInteractive() {
		return nil
	}

	ctx.Printf("\n")

	// Help picker
	for {
		items := []ui.PickerItem{
			ui.SimplePickerItem{Text: "How do I add an existing cluster?"},
			ui.SimplePickerItem{Text: "How do I register a new cluster?"},
			ui.SimplePickerItem{Text: "[done]"},
		}

		selected, err := ui.RunPicker(items, ui.WithTitle("Need help?"))
		if err != nil || selected == nil {
			return nil
		}

		switch selected.ID() {
		case "How do I add an existing cluster?":
			showAddClusterHelp(ctx, cfg)

		case "How do I register a new cluster?":
			showRegisterClusterHelp(ctx, existing)

		case "[done]":
			return nil
		}
	}
}

func showAddClusterHelp(ctx *Context, cfg *clientconfig.Config) {
	ctx.Printf("\n%s\n", infoLabel.Render("Adding an existing cluster to your config"))
	ctx.Printf("%s\n\n", infoGray.Render("──────────────────────────────────────────"))

	// Check if logged in
	hasIdentity := cfg != nil && cfg.HasIdentities()

	if !hasIdentity {
		ctx.Printf("%s\n", infoLabel.Render("Step 1: Login to miren.cloud"))
		ctx.Printf("  %s\n\n", infoBold.Render("miren login"))
	}

	ctx.Printf("%s\n", infoLabel.Render("Add a cluster from miren.cloud:"))
	ctx.Printf("  %s\n\n", infoBold.Render("miren cluster add"))

	ctx.Printf("%s\n", infoLabel.Render("Or add a cluster manually by address:"))
	ctx.Printf("  %s\n\n", infoBold.Render("miren cluster add -a <hostname:port>"))

	ctx.Printf("%s\n", infoLabel.Render("Switch between clusters:"))
	ctx.Printf("  %s\n", infoBold.Render("miren cluster switch <name>"))

	ctx.Printf("\n%s", infoGray.Render("Press Enter to continue..."))
	fmt.Scanln()
	ctx.Printf("\n")
}

func showRegisterClusterHelp(ctx *Context, existing *registration.StoredRegistration) {
	ctx.Printf("\n%s\n", infoLabel.Render("Registering a new cluster with miren.cloud"))
	ctx.Printf("%s\n\n", infoGray.Render("──────────────────────────────────────────"))

	// Show current registration status
	if existing != nil && existing.Status == "approved" {
		ctx.Printf("%s\n", infoLabel.Render("Current registration:"))
		ctx.Printf("  Cluster: %s\n", existing.ClusterName)
		ctx.Printf("  ID: %s\n", existing.ClusterID)
		ctx.Printf("  Org: %s\n\n", existing.OrganizationID)

		ctx.Printf("%s\n", infoLabel.Render("To register as a different cluster:"))
		ctx.Printf("  %s  %s\n", infoBold.Render("1."), "Stop the miren server")
		ctx.Printf("     %s\n\n", infoGray.Render("sudo systemctl stop miren"))
		ctx.Printf("  %s  %s\n", infoBold.Render("2."), "Delete the existing registration")
		ctx.Printf("     %s\n\n", infoGray.Render("sudo rm /var/lib/miren/server/registration.json"))
		ctx.Printf("  %s  %s\n", infoBold.Render("3."), "Register with a new name")
		ctx.Printf("     %s\n\n", infoGray.Render("sudo miren server register -n <cluster-name>"))
		ctx.Printf("  %s  %s\n", infoBold.Render("4."), "Approve in browser when prompted")
		ctx.Printf("\n")
		ctx.Printf("  %s  %s\n", infoBold.Render("5."), "Restart the server")
		ctx.Printf("     %s\n\n", infoGray.Render("sudo systemctl start miren"))
		ctx.Printf("  %s  %s\n", infoBold.Render("6."), "Add the cluster to your config")
		ctx.Printf("     %s\n", infoGray.Render("miren cluster add"))
	} else {
		ctx.Printf("%s\n", infoLabel.Render("To register this server as a cluster:"))
		ctx.Printf("  %s  %s\n", infoBold.Render("1."), "Register with miren.cloud")
		ctx.Printf("     %s\n\n", infoGray.Render("sudo miren server register -n <cluster-name>"))
		ctx.Printf("  %s  %s\n", infoBold.Render("2."), "Approve in browser when prompted")
		ctx.Printf("\n")
		ctx.Printf("  %s  %s\n", infoBold.Render("3."), "Restart the server to apply registration")
		ctx.Printf("     %s\n\n", infoGray.Render("sudo systemctl restart miren"))
		ctx.Printf("  %s  %s\n", infoBold.Render("4."), "Add the cluster to your config")
		ctx.Printf("     %s\n\n", infoGray.Render("miren cluster add"))

		ctx.Printf("%s\n", infoGray.Render("If registration fails, you may have to delete the registration file at:"))
		ctx.Printf("  %s\n", infoGray.Render("/var/lib/miren/server/registration.json"))
	}

	ctx.Printf("\n%s", infoGray.Render("Press Enter to continue..."))
	fmt.Scanln()
	ctx.Printf("\n")
}
