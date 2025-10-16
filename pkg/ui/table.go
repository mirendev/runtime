// Package ui provides common UI components for the miren CLI
package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// Column defines a table column with title and width
type Column struct {
	Title string
	Width int
}

// Row represents a single row of data in the table
type Row []string

// Table represents a simple, non-interactive table for CLI output
type Table struct {
	columns []Column
	rows    []Row
	styles  TableStyles
}

// TableStyles contains the styling configuration for the table
type TableStyles struct {
	Header lipgloss.Style
	Cell   lipgloss.Style
}

// DefaultTableStyles returns the default styling for tables
func DefaultTableStyles() TableStyles {
	return TableStyles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			UnderlineSpaces(true).
			Foreground(lipgloss.Color("220")),
		Cell: lipgloss.NewStyle(),
	}
}

// TableOption is a function that configures a table
type TableOption func(*Table)

// WithColumns sets the columns for the table
func WithColumns(cols []Column) TableOption {
	return func(t *Table) {
		t.columns = cols
	}
}

// WithRows sets the rows for the table
func WithRows(rows []Row) TableOption {
	return func(t *Table) {
		t.rows = rows
	}
}

// WithStyles sets custom styles for the table
func WithStyles(styles TableStyles) TableOption {
	return func(t *Table) {
		t.styles = styles
	}
}

// NewTable creates a new table with the given options
func NewTable(opts ...TableOption) *Table {
	t := &Table{
		styles: DefaultTableStyles(),
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// AutoSizeColumns calculates optimal column widths based on content
// with optional maximum widths per column, respecting terminal width
func AutoSizeColumns(headers []string, rows []Row, maxWidths ...int) []Column {
	if len(headers) == 0 {
		return nil
	}

	// Check if stdout is a terminal
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	// Get terminal width only if stdout is a TTY
	var termWidth int
	if isTTY {
		if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
			termWidth = width
		} else {
			termWidth = 80 // fallback for TTY
		}
	}
	// If not a TTY (piped/redirected), don't constrain width

	// Initialize widths with header lengths
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = lipgloss.Width(header)
	}

	// Check all row values
	for _, row := range rows {
		for i, value := range row {
			if i < len(widths) {
				displayWidth := measureDisplayWidth(value)
				if displayWidth > widths[i] {
					widths[i] = displayWidth
				}
			}
		}
	}

	// Apply max widths if provided
	for i := range widths {
		if i < len(maxWidths) && maxWidths[i] > 0 {
			widths[i] = min(widths[i], maxWidths[i])
		}
	}

	// Only apply terminal width constraints if stdout is a TTY
	if isTTY && termWidth > 0 {
		// Calculate total width needed (including spaces between columns)
		totalWidth := 0
		for _, w := range widths {
			totalWidth += w
		}
		totalWidth += (len(headers) - 1) * 2 // 2 spaces between each column

		// If total width exceeds terminal width, scale down proportionally
		if totalWidth > termWidth {
			availableWidth := termWidth - (len(headers)-1)*2 // subtract space for margins

			// Calculate scaling factor
			scaleFactor := float64(availableWidth) / float64(totalWidth-(len(headers)-1)*2)

			// Scale each column width
			for i := range widths {
				newWidth := int(float64(widths[i]) * scaleFactor)
				if newWidth < 10 { // Minimum column width
					newWidth = 10
				}
				widths[i] = newWidth
			}
		}
	}

	// Create columns
	columns := make([]Column, len(headers))
	for i, header := range headers {
		columns[i] = Column{
			Title: header,
			Width: widths[i],
		}
	}

	return columns
}

// measureDisplayWidth measures the display width of a string,
// accounting for ANSI escape sequences
func measureDisplayWidth(s string) int {
	// Strip ANSI escape sequences to get the actual display width
	// lipgloss.Width handles this correctly
	return lipgloss.Width(s)
}

// Render generates the string representation of the table
func (t *Table) Render() string {
	if len(t.columns) == 0 || len(t.rows) == 0 {
		return ""
	}

	var lines []string

	// Render header
	lines = append(lines, t.renderHeader())

	// Render rows
	for _, row := range t.rows {
		lines = append(lines, t.renderRow(row))
	}

	return strings.Join(lines, "\n")
}

// renderHeader renders the table header
func (t *Table) renderHeader() string {
	cells := make([]string, 0, len(t.columns)*2-1) // Account for spacers

	for i, col := range t.columns {
		if col.Width <= 0 {
			continue
		}

		// Add spacing between columns (but not before the first)
		if i > 0 {
			cells = append(cells, "  ") // Two spaces between columns
		}

		// Create a style for the cell content with fixed width
		cellStyle := lipgloss.NewStyle().
			Width(col.Width).
			MaxWidth(col.Width).
			Inline(true)

		// Truncate with ellipsis and render
		truncated := runewidth.Truncate(col.Title, col.Width, "…")
		renderedCell := cellStyle.Render(truncated)

		// Apply header styling
		cells = append(cells, t.styles.Header.Render(renderedCell))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// renderRow renders a single data row
func (t *Table) renderRow(row Row) string {
	cells := make([]string, 0, len(t.columns)*2-1) // Account for spacers

	for i, col := range t.columns {
		if col.Width <= 0 {
			continue
		}

		// Add spacing between columns (but not before the first)
		if i > 0 {
			cells = append(cells, "  ") // Two spaces between columns
		}

		// Get the value for this column
		value := ""
		if i < len(row) {
			value = row[i]
		}

		// Create a style for the cell content with fixed width
		cellStyle := lipgloss.NewStyle().
			Width(col.Width).
			MaxWidth(col.Width).
			Inline(true)

		// For already-styled content (like gray sensitive values), we can't easily truncate
		// because runewidth.Truncate doesn't handle ANSI codes well.
		// So we only truncate plain text values.
		// We can detect styled content by checking if it contains ANSI escape sequences.
		var renderedCell string
		if strings.Contains(value, "\x1b[") {
			// Already styled - let lipgloss handle width constraints
			renderedCell = cellStyle.Render(value)
		} else {
			// Plain text - truncate with ellipsis
			truncated := runewidth.Truncate(value, col.Width, "…")
			renderedCell = cellStyle.Render(truncated)
		}

		// Apply cell styling
		cells = append(cells, t.styles.Cell.Render(renderedCell))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}
