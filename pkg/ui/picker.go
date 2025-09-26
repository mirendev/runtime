package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PickerItem represents an item that can be selected in the picker
type PickerItem interface {
	// Row returns the table row data for this item
	Row() []string
	// ID returns a unique identifier for this item
	ID() string
}

// SimplePickerItem is a basic implementation of PickerItem for single-column pickers
type SimplePickerItem struct {
	Text   string
	Active bool
}

func (s SimplePickerItem) Row() []string {
	if s.Active {
		return []string{fmt.Sprintf("%s *", s.Text)}
	}
	return []string{s.Text}
}

func (s SimplePickerItem) ID() string {
	return s.Text
}

// TablePickerItem is a multi-column implementation of PickerItem
type TablePickerItem struct {
	Columns []string
	ItemID  string
}

func (t TablePickerItem) Row() []string {
	return t.Columns
}

func (t TablePickerItem) ID() string {
	return t.ItemID
}

// PickerModel is a table-based picker for selecting from a list of items
type PickerModel struct {
	Title     string
	Headers   []string
	Items     []PickerItem
	cursor    int
	Selected  PickerItem
	Cancelled bool
	Footer    string

	// Optional filter function to disable certain items
	IsDisabled func(item PickerItem) bool
	// Optional message for disabled items
	DisabledMessage string
}

func (m *PickerModel) Init() tea.Cmd {
	return nil
}

func (m *PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.Cancelled = true
			return m, tea.Quit

		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.Items) {
				item := m.Items[m.cursor]
				// Check if item is disabled
				if m.IsDisabled != nil && m.IsDisabled(item) {
					// Don't select disabled items
					return m, nil
				}
				m.Selected = item
			}
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

func (m *PickerModel) View() string {
	if len(m.Items) == 0 {
		return "No items to select"
	}

	var b strings.Builder

	if m.Title != "" {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229"))
		b.WriteString(titleStyle.Render(m.Title))
		b.WriteString("\n\n")
	}

	// Prepare rows for the table - add selection column
	rows := make([]Row, len(m.Items))
	for i, item := range m.Items {
		itemRow := item.Row()
		// Prepend selection indicator column
		row := make([]string, len(itemRow)+1)
		if i == m.cursor {
			row[0] = "▸"
		} else {
			row[0] = " "
		}
		copy(row[1:], itemRow)
		rows[i] = row
	}

	// Calculate columns - add empty header for selection column
	headers := m.Headers
	if len(headers) == 0 {
		// No headers provided, create empty headers based on first row
		if len(rows) > 0 {
			headers = make([]string, len(rows[0]))
		}
	} else {
		// Prepend empty header for selection column
		newHeaders := make([]string, len(headers)+1)
		newHeaders[0] = ""
		copy(newHeaders[1:], headers)
		headers = newHeaders
	}

	// Auto-size columns
	columns := AutoSizeColumns(headers, rows)

	// Style for selected row
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("170")).
		Bold(true)

	// Style for disabled rows
	disabledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Render the table with custom row styling
	var tableLines []string

	// Render headers if they exist and are not empty
	hasHeaders := false
	for _, h := range headers {
		if h != "" {
			hasHeaders = true
			break
		}
	}

	if hasHeaders {
		headerCells := make([]string, 0, len(columns)*2-1)
		for i, col := range columns {
			if col.Width <= 0 {
				continue
			}
			if i > 0 {
				headerCells = append(headerCells, "  ")
			}
			// First apply width constraints
			cellStyle := lipgloss.NewStyle().
				Width(col.Width).
				MaxWidth(col.Width).
				Inline(true)
			
			// Render the cell with width constraints
			renderedCell := cellStyle.Render(col.Title)
			
			// Then apply header styling (underline, bold, color)
			headerStyle := lipgloss.NewStyle().
				Bold(true).
				Underline(true).
				UnderlineSpaces(true).
				Foreground(lipgloss.Color("220"))
			
			headerCells = append(headerCells, headerStyle.Render(renderedCell))
		}
		tableLines = append(tableLines, lipgloss.JoinHorizontal(lipgloss.Top, headerCells...))
	}

	// Render rows with selection highlight
	for i, row := range rows {
		cells := make([]string, 0, len(columns)*2-1)

		// Determine if this row is disabled
		isDisabled := m.IsDisabled != nil && m.IsDisabled(m.Items[i])

		for j, col := range columns {
			if col.Width <= 0 {
				continue
			}
			if j > 0 {
				cells = append(cells, "  ")
			}

			value := ""
			if j < len(row) {
				value = row[j]
			}

			cellStyle := lipgloss.NewStyle().
				Width(col.Width).
				MaxWidth(col.Width).
				Inline(true)

			// Apply selection or disabled styling
			if isDisabled {
				cellStyle = cellStyle.Inherit(disabledStyle)
			} else if i == m.cursor {
				cellStyle = cellStyle.Inherit(selectedStyle)
			}

			cells = append(cells, cellStyle.Render(value))
		}

		tableLines = append(tableLines, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	b.WriteString(strings.Join(tableLines, "\n"))

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	helpText := "\n(Use ↑/↓ or j/k to navigate, Enter to select, Esc to cancel)"
	b.WriteString(helpStyle.Render(helpText))

	if m.Footer != "" {
		footerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
		b.WriteString("\n")
		b.WriteString(footerStyle.Render(m.Footer))
	}

	// Reserve space for disabled message to prevent jumping
	if m.IsDisabled != nil && m.DisabledMessage != "" {
		b.WriteString("\n")
		if m.cursor < len(m.Items) && m.IsDisabled(m.Items[m.cursor]) {
			// Show the warning message
			msgStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))
			b.WriteString(msgStyle.Render("⚠ " + m.DisabledMessage))
		} else {
			// Keep the space reserved but empty
			b.WriteString(" ")
		}
	}

	return b.String()
}

// SetCursor sets the cursor to the specified index
func (m *PickerModel) SetCursor(index int) {
	if index >= 0 && index < len(m.Items) {
		m.cursor = index
	}
}

// Picker configuration options
type PickerOption func(*PickerModel)

// WithTitle sets the picker title
func WithTitle(title string) PickerOption {
	return func(m *PickerModel) {
		m.Title = title
	}
}

// WithHeaders sets the table headers for the picker
func WithHeaders(headers []string) PickerOption {
	return func(m *PickerModel) {
		m.Headers = headers
	}
}

// WithFooter sets the picker footer text
func WithFooter(footer string) PickerOption {
	return func(m *PickerModel) {
		m.Footer = footer
	}
}

// WithDisabledCheck sets a function to determine if items are disabled
func WithDisabledCheck(check func(PickerItem) bool, message string) PickerOption {
	return func(m *PickerModel) {
		m.IsDisabled = check
		m.DisabledMessage = message
	}
}

// NewPicker creates a new picker model with the given options
func NewPicker(items []PickerItem, opts ...PickerOption) *PickerModel {
	m := &PickerModel{
		Items: items,
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	return m
}

// RunPicker runs an interactive picker and returns the selected item
func RunPicker(items []PickerItem, opts ...PickerOption) (PickerItem, error) {
	// Check if we can run interactive mode
	if !IsInteractive() {
		return nil, fmt.Errorf("interactive mode not available")
	}

	model := NewPicker(items, opts...)

	p := tea.NewProgram(model)
	result, err := p.Run()
	if err != nil {
		return nil, err
	}

	m := result.(*PickerModel)
	if m.Cancelled {
		return nil, nil
	}

	return m.Selected, nil
}

// IsInteractive checks if we're in an interactive terminal
func IsInteractive() bool {
	if os.Getenv("CI") != "" {
		return false
	}

	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
