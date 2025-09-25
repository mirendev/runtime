package commands

import (
	"net"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// ClusterList lists all configured clusters (replaces config info)
func ClusterList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	// Prepare structured data
	type ClusterInfo struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		Identity string `json:"identity"`
		Active   bool   `json:"active"`
	}

	var clusters []ClusterInfo
	var rows []ui.Row
	headers := []string{"", "CLUSTER", "ADDRESS", "IDENTITY"}

	err = cfg.IterateClusters(func(name string, ccfg *clientconfig.ClusterConfig) error {
		// Determine if this is the active cluster
		isActive := false
		if opts.Cluster != "" {
			isActive = (name == opts.Cluster)
		} else {
			isActive = (name == cfg.ActiveCluster())
		}

		// Use a star for active cluster
		prefix := " "
		if isActive {
			prefix = "*"
		}

		// Get identity info if present
		identity := ccfg.Identity
		if identity == "" {
			identity = "-"
		}

		// Build structured data for JSON
		clusterInfo := ClusterInfo{
			Name:     name,
			Address:  ccfg.Hostname,
			Identity: ccfg.Identity,
			Active:   isActive,
		}
		clusters = append(clusters, clusterInfo)

		// Build table row with formatting
		if !opts.IsJSON() {
			// Format address - color port portion gray for table display
			address := ccfg.Hostname
			if host, port, err := net.SplitHostPort(address); err == nil {
				grayPort := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(":" + port)
				address = host + grayPort
			}

			rows = append(rows, ui.Row{
				prefix,
				name,
				address,
				identity,
			})
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Output based on format
	if opts.IsJSON() {
		return PrintJSON(clusters)
	}

	if len(clusters) == 0 {
		ctx.Printf("No clusters configured\n")
		ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
		return nil
	}

	// Create and render the table
	columns := ui.AutoSizeColumns(headers, rows)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

// Cluster is the default command for the cluster group - shows the list
func Cluster(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	return ClusterList(ctx, opts)
}
