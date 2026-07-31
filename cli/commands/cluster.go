package commands

import (
	"errors"
	"net"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// ClusterList lists all configured clusters (replaces config info)
func ClusterList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		// Handle missing config gracefully
		if errors.Is(err, clientconfig.ErrNoConfig) {
			if opts.IsJSON() {
				// Return empty array for JSON
				type ClusterInfo struct {
					Name     string `json:"name"`
					Address  string `json:"address"`
					Identity string `json:"identity"`
					Active   bool   `json:"active"`
					Source   string `json:"source"`
				}
				return PrintJSON([]ClusterInfo{})
			}
			ctx.Printf("No clusters configured\n")
			ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
			return nil
		}
		return err
	}

	// Prepare structured data
	type ClusterInfo struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		Identity string `json:"identity"`
		Active   bool   `json:"active"`
		// Source is the config file the cluster came from, which is the one
		// thing `doctor config` used to show that nothing else did. It answers
		// "why is this cluster even here?" when an unexpected one shows up.
		Source string `json:"source"`
	}

	var clusters []ClusterInfo
	var rows []ui.Row
	headers := []string{"", "CLUSTER", "ADDRESS", "IDENTITY", "SOURCE"}

	// Determine the effective cluster for the current context.
	globalCluster := cfg.ActiveCluster()
	_, effectiveCluster, _ := opts.LoadCluster()
	if effectiveCluster == "" {
		effectiveCluster = globalCluster
	}

	err = cfg.IterateClusters(func(name string, ccfg *clientconfig.ClusterConfig) error {
		// Use a star for the effective cluster, g for the global default when it differs.
		prefix := " "
		if name == effectiveCluster {
			prefix = "*"
		} else if name == globalCluster && effectiveCluster != globalCluster {
			prefix = "g"
		}

		isActive := name == effectiveCluster

		// Get identity info if present
		identity := ccfg.Identity
		if identity == "" {
			identity = "-"
		}

		// Build structured data for JSON
		source := cfg.GetClusterSource(name)

		clusterInfo := ClusterInfo{
			Name:     name,
			Address:  ccfg.Hostname,
			Identity: ccfg.Identity,
			Active:   isActive,
			Source:   source,
		}
		clusters = append(clusters, clusterInfo)

		// Build table row with formatting
		if !opts.IsJSON() {
			// Format address - color port portion gray for table display
			address := ccfg.Hostname
			if host, port, err := net.SplitHostPort(address); err == nil {
				grayPort := lipgloss.NewStyle().Foreground(theme.Muted).Render(":" + port)
				address = host + grayPort
			}

			rows = append(rows, ui.Row{
				prefix,
				name,
				address,
				identity,
				shortenHomePath(source),
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
	columns := ui.AutoSizeColumns(headers, rows, nil)
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

// shortenHomePath replaces the home directory prefix with ~ so config paths fit
// in a table column without wrapping.
//
// The prefix must end at a directory boundary. A bare strings.HasPrefix turns
// /home/alice-work/config.yaml into ~-work/config.yaml when home is
// /home/alice, which isn't a path anyone can use.
func shortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := strings.TrimSuffix(home, string(os.PathSeparator)) + string(os.PathSeparator)
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	return "~" + string(os.PathSeparator) + path[len(prefix):]
}
