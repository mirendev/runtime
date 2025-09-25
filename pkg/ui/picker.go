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
	// String returns the display text for the item
	String() string
	// IsActive returns true if this is the currently active/selected item
	IsActive() bool
}

// SimplePickerItem is a basic implementation of PickerItem
type SimplePickerItem struct {
	Text   string
	Active bool
}

func (s SimplePickerItem) String() string {
	return s.Text
}

func (s SimplePickerItem) IsActive() bool {
	return s.Active
}

// PickerModel is a generic model for selecting from a list of items
type PickerModel struct {
	Title     string
	Items     []PickerItem
	cursor    int
	Selected  PickerItem
	Cancelled bool
	Footer    string // Optional footer text

	// Optional styling/marking functions
	ItemMarker func(item PickerItem) string         // Returns marker like " *" for special items
	ItemStyle  func(item PickerItem) lipgloss.Style // Returns style for special items
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
				m.Selected = m.Items[m.cursor]
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

		line := fmt.Sprintf("%s %s%s", cursor, item.String(), marker)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n(Use arrow keys to navigate, enter to select, esc to cancel)\n")

	if m.Footer != "" {
		b.WriteString(m.Footer + "\n")
	}

	return b.String()
}

// SetCursor sets the cursor to the specified index
func (m *PickerModel) SetCursor(index int) {
	if index >= 0 && index < len(m.Items) {
		m.cursor = index
	}
}

// SetCursorToActive sets the cursor to the first active item
func (m *PickerModel) SetCursorToActive() {
	for i, item := range m.Items {
		if item.IsActive() {
			m.cursor = i
			return
		}
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

// WithFooter sets the picker footer text
func WithFooter(footer string) PickerOption {
	return func(m *PickerModel) {
		m.Footer = footer
	}
}

// WithActiveMarker sets a function to mark active items (e.g., with " *")
func WithActiveMarker() PickerOption {
	return func(m *PickerModel) {
		m.ItemMarker = func(item PickerItem) string {
			if item.IsActive() {
				return " *"
			}
			return "  "
		}
	}
}

// WithDimmedActiveStyle dims the active item (useful when it cannot be selected)
func WithDimmedActiveStyle() PickerOption {
	return func(m *PickerModel) {
		m.ItemStyle = func(item PickerItem) lipgloss.Style {
			if item.IsActive() {
				return lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			}
			return lipgloss.NewStyle()
		}
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

	// Set cursor to active item by default
	m.SetCursorToActive()

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
