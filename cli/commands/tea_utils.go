package commands

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SelectionModel is a generic model for selecting from a list of items
type SelectionModel struct {
	Title     string
	Items     []string
	cursor    int
	Selected  string
	Cancelled bool
	Footer    string // Optional footer text

	// Optional styling/marking functions
	ItemMarker func(item string) string         // Returns marker like " *" for special items
	ItemStyle  func(item string) lipgloss.Style // Returns style for special items
}

func (m *SelectionModel) Init() tea.Cmd {
	return nil
}

func (m *SelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.Cancelled = true
			return m, tea.Quit

		case "enter", " ":
			m.Selected = m.Items[m.cursor]
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.Items)-1 {
				m.cursor++
			}
		}
	}

	return m, nil
}

func (m *SelectionModel) View() string {
	var b strings.Builder

	b.WriteString(m.Title + "\n\n")

	for i, item := range m.Items {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		marker := "  "
		if m.ItemMarker != nil {
			marker = m.ItemMarker(item)
		}

		// Start with base style
		style := lipgloss.NewStyle()

		// Apply custom item style if provided
		if m.ItemStyle != nil {
			style = m.ItemStyle(item)
		}

		// Apply selection highlighting
		if m.cursor == i {
			// Override with selection color unless item already has custom styling
			if m.ItemStyle == nil {
				style = style.Foreground(lipgloss.Color("170"))
			} else {
				// If there's custom styling, just make it bold
				style = style.Bold(true).Foreground(lipgloss.Color("170"))
			}
		}

		line := fmt.Sprintf("%s %s%s", cursor, item, marker)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n(Use arrow keys to navigate, enter to select, esc to cancel)\n")

	if m.Footer != "" {
		b.WriteString(m.Footer + "\n")
	}

	return b.String()
}

// SetCursor sets the cursor to the specified index
func (m *SelectionModel) SetCursor(index int) {
	if index >= 0 && index < len(m.Items) {
		m.cursor = index
	}
}

// Helper function to run cluster selection
func SelectCluster(ctx *Context, title string, clusters []string, activeCluster string, dimActive bool) (string, error) {
	model := &SelectionModel{
		Title: title,
		Items: clusters,
		ItemMarker: func(item string) string {
			if item == activeCluster {
				return " *"
			}
			return "  "
		},
	}

	if dimActive {
		model.ItemStyle = func(item string) lipgloss.Style {
			if item == activeCluster {
				return lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			}
			return lipgloss.NewStyle()
		}
		model.Footer = "Note: You cannot remove the active cluster"
	}

	// Find current active cluster index
	for i, name := range clusters {
		if name == activeCluster {
			model.SetCursor(i)
			break
		}
	}

	p := tea.NewProgram(model)
	result, err := p.Run()
	if err != nil {
		return "", err
	}

	m := result.(*SelectionModel)
	if m.Cancelled {
		return "", nil
	}

	return m.Selected, nil
}
