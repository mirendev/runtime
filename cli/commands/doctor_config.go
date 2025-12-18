package commands

import (
	"errors"
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

	// Helper to render cluster table
	renderClusterTable := func(label string, clusters []clusterInfo) {
		if len(clusters) == 0 {
			return
		}
		ctx.Printf("\n%s\n", infoLabel.Render(label))
		headers := []string{"CLUSTER", "ADDRESS", "SOURCE FILE"}
		var rows []ui.Row
		for _, c := range clusters {
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

	renderClusterTable("Local:", localClusters)
	renderClusterTable("Remote:", remoteClusters)

	if len(localClusters) > 0 || len(remoteClusters) > 0 {
		ctx.Printf("%s\n", infoGray.Render("* = active cluster"))
	}

	// Interactive mode
	if !ui.IsInteractive() {
		return nil
	}

	// Load registration only when needed for interactive help
	existing, _ := registration.LoadRegistration("/var/lib/miren/server")

	ctx.Printf("\n")

	// Help picker
	for {
		items := []ui.PickerItem{
			ui.SimplePickerItem{Text: "How do I add an existing cluster?"},
			ui.SimplePickerItem{Text: "How do I register a new cluster?"},
			ui.SimplePickerItem{Text: "[done]"},
		}

		selected, err := ui.RunPicker(items, ui.WithTitle("Need help?"))
		if err != nil || selected == nil || selected.ID() == "[done]" {
			return nil
		}

		switch selected.ID() {
		case "How do I add an existing cluster?":
			showAddClusterHelp(ctx, cfg)
		case "How do I register a new cluster?":
			showRegisterClusterHelp(ctx, existing)
		}
	}
}

func showAddClusterHelp(ctx *Context, cfg *clientconfig.Config) {
	printHelpHeader(ctx, "Adding an existing cluster to your config")

	// Check if logged in
	if cfg == nil || !cfg.HasIdentities() {
		printCommand(ctx, "Step 1: Login to miren.cloud", "miren login")
	}

	printCommand(ctx, "Add a cluster from miren.cloud:", "miren cluster add")
	printCommand(ctx, "Or add a cluster manually by address:", "miren cluster add -a <hostname:port>")
	ctx.Printf("%s\n", infoLabel.Render("Switch between clusters:"))
	ctx.Printf("  %s\n", infoGray.Render("miren cluster switch <name>"))
	waitForEnter(ctx)
}

func showRegisterClusterHelp(ctx *Context, existing *registration.StoredRegistration) {
	printHelpHeader(ctx, "Registering a new cluster with miren.cloud")

	// Show current registration status
	if existing != nil && existing.Status == "approved" {
		ctx.Printf("%s\n", infoLabel.Render("Current registration:"))
		ctx.Printf("  Cluster: %s\n", existing.ClusterName)
		ctx.Printf("  ID: %s\n", existing.ClusterID)
		ctx.Printf("  Org: %s\n\n", existing.OrganizationID)

		ctx.Printf("%s\n", infoLabel.Render("To register as a different cluster:"))
		printNumberedStep(ctx, "1", "Stop the miren server", "sudo systemctl stop miren")
		printNumberedStep(ctx, "2", "Delete the existing registration", "sudo rm /var/lib/miren/server/registration.json")
		printNumberedStep(ctx, "3", "Register with a new name", "sudo miren server register -n <cluster-name>")
		ctx.Printf("  %s  %s\n\n", infoBold.Render("4."), "Approve in browser when prompted")
		printNumberedStep(ctx, "5", "Restart the server", "sudo systemctl start miren")
		ctx.Printf("  %s  %s\n", infoBold.Render("6."), "Add the cluster to your config")
		ctx.Printf("     %s\n", infoGray.Render("miren cluster add"))
	} else {
		ctx.Printf("%s\n", infoLabel.Render("To register this server as a cluster:"))
		printNumberedStep(ctx, "1", "Register with miren.cloud", "sudo miren server register -n <cluster-name>")
		ctx.Printf("  %s  %s\n\n", infoBold.Render("2."), "Approve in browser when prompted")
		printNumberedStep(ctx, "3", "Restart the server to apply registration", "sudo systemctl restart miren")
		ctx.Printf("  %s  %s\n", infoBold.Render("4."), "Add the cluster to your config")
		ctx.Printf("     %s\n\n", infoGray.Render("miren cluster add"))

		ctx.Printf("%s\n", infoGray.Render("If registration fails, you may have to delete the registration file at:"))
		ctx.Printf("  %s\n", infoGray.Render("/var/lib/miren/server/registration.json"))
	}

	waitForEnter(ctx)
}
