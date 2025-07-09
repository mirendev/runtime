package commands

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func ConfigSetActive(ctx *Context, opts struct {
	ConfigCentric
	Args struct {
		Cluster string `positional-arg-name:"cluster" description:"Name of the cluster to set as active"`
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
		clusterNames := make([]string, 0, len(cfg.Clusters))
		for name := range cfg.Clusters {
			clusterNames = append(clusterNames, name)
		}
		sort.Strings(clusterNames)

		// Find current active cluster index
		currentIndex := 0
		for i, name := range clusterNames {
			if name == cfg.ActiveCluster {
				currentIndex = i
				break
			}
		}

		// Create and run the selection model
		m := &clusterSelectModel{
			clusters: clusterNames,
			cursor:   currentIndex,
			active:   cfg.ActiveCluster,
		}

		p := tea.NewProgram(m)
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("failed to run selection menu: %w", err)
		}

		// Check if user cancelled
		model := result.(*clusterSelectModel)
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

// clusterSelectModel is the model for the cluster selection menu
type clusterSelectModel struct {
	clusters  []string
	cursor    int
	selected  string
	cancelled bool
	active    string
}

func (m *clusterSelectModel) Init() tea.Cmd {
	return nil
}

func (m *clusterSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *clusterSelectModel) View() string {
	var b strings.Builder

	b.WriteString("Select a cluster to set as active:\n\n")

	for i, cluster := range m.clusters {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		activeMarker := "  "
		if cluster == m.active {
			activeMarker = " *"
		}

		style := lipgloss.NewStyle()
		if m.cursor == i {
			style = style.Foreground(lipgloss.Color("170"))
		}

		line := fmt.Sprintf("%s %s%s", cursor, cluster, activeMarker)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n(Use arrow keys to navigate, enter to select, esc to cancel)\n")

	return b.String()
}
