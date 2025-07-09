package commands

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	if len(cfg.Clusters) <= 1 {
		return fmt.Errorf("cannot remove the last cluster")
	}

	clusterName := opts.Args.Cluster

	// If no cluster name provided, show interactive menu
	if clusterName == "" {
		// Get sorted list of cluster names
		clusterNames := make([]string, 0, len(cfg.Clusters))
		for name := range cfg.Clusters {
			clusterNames = append(clusterNames, name)
		}
		sort.Strings(clusterNames)

		// Create and run the selection model
		m := &clusterRemoveModel{
			clusters: clusterNames,
			cursor:   0,
			active:   cfg.ActiveCluster,
		}

		p := tea.NewProgram(m)
		result, err := p.Run()
		if err != nil {
			// If we can't run interactive mode (no TTY), show available clusters
			ctx.Printf("Cannot run interactive mode. Available clusters:\n")
			for _, name := range clusterNames {
				prefix := "  "
				if name == cfg.ActiveCluster {
					prefix = "* "
				}
				ctx.Printf("%s%s\n", prefix, name)
			}
			ctx.Printf("\nUsage: runtime config remove <cluster-name>\n")
			return nil
		}

		// Check if user cancelled
		model := result.(*clusterRemoveModel)
		if model.cancelled {
			return nil
		}

		clusterName = model.selected
	}

	// Check if the cluster exists
	if _, exists := cfg.Clusters[clusterName]; !exists {
		availableClusters := make([]string, 0, len(cfg.Clusters))
		for name := range cfg.Clusters {
			availableClusters = append(availableClusters, name)
		}
		return fmt.Errorf("cluster %q not found. Available clusters: %v", clusterName, availableClusters)
	}

	// Check if trying to remove the active cluster
	if clusterName == cfg.ActiveCluster {
		return fmt.Errorf("cannot remove the active cluster %q. Please switch to another cluster first", clusterName)
	}

	// Ask for confirmation unless --force is used
	if !opts.Force {
		ctx.Printf("This will remove cluster '%s' from your configuration.\n", clusterName)
		ctx.Printf("To confirm, run the command with --force flag\n")
		return nil
	}

	// Remove the cluster
	delete(cfg.Clusters, clusterName)

	// Save the configuration
	err = cfg.Save()
	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	ctx.Printf("Removed cluster: %s\n", clusterName)
	return nil
}

// clusterRemoveModel is the model for the cluster removal selection menu
type clusterRemoveModel struct {
	clusters  []string
	cursor    int
	selected  string
	cancelled bool
	active    string
}

func (m *clusterRemoveModel) Init() tea.Cmd {
	return nil
}

func (m *clusterRemoveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "enter", " ":
			m.selected = m.clusters[m.cursor]
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.clusters)-1 {
				m.cursor++
			}
		}
	}

	return m, nil
}

func (m *clusterRemoveModel) View() string {
	var b strings.Builder

	b.WriteString("Select a cluster to remove:\n\n")

	for i, cluster := range m.clusters {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		activeMarker := "  "
		if cluster == m.active {
			activeMarker = " * (active)"
		}

		style := lipgloss.NewStyle()
		if m.cursor == i {
			style = style.Foreground(lipgloss.Color("170"))
		}

		// Dim the active cluster to indicate it can't be removed
		if cluster == m.active {
			style = style.Foreground(lipgloss.Color("240"))
		}

		line := fmt.Sprintf("%s %s%s", cursor, cluster, activeMarker)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n(Use arrow keys to navigate, enter to select, esc to cancel)\n")
	b.WriteString("Note: You cannot remove the active cluster\n")

	return b.String()
}
