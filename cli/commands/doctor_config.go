package commands

import (
	"errors"
	"net"
	"os"
	"strings"

	"miren.dev/runtime/clientconfig"
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

	// Show help content in interactive mode
	if !ui.IsInteractive() {
		return nil
	}

	ctx.Printf("\n")
	showAddClusterHelp(ctx, cfg)
	showRegisterClusterHelp(ctx)

	return nil
}

func showAddClusterHelp(ctx *Context, cfg *clientconfig.Config) {
	ctx.Printf("%s\n", infoBold.Render("How do I add an existing cluster?"))
	if cfg == nil || !cfg.HasIdentities() {
		ctx.Printf("  %s\n", infoGray.Render("miren login"))
	}
	ctx.Printf("  %s\n", infoGray.Render("miren cluster add"))
	ctx.Printf("  %s\n\n", infoGray.Render("miren cluster switch <name>"))
}

func showRegisterClusterHelp(ctx *Context) {
	ctx.Printf("%s\n", infoBold.Render("How do I register a new cluster?"))
	ctx.Printf("  %s\n", infoGray.Render("sudo miren server register -n <cluster-name>"))
	ctx.Printf("  %s\n", infoGray.Render("# Approve in browser when prompted"))
	ctx.Printf("  %s\n", infoGray.Render("sudo systemctl restart miren"))
	ctx.Printf("  %s\n", infoGray.Render("miren cluster add"))
}
