// Package ui provides common UI components for the miren CLI
package ui

import (
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// oscPattern matches OSC 8 hyperlink sequences: ESC ] 8 ; ; URL ST TEXT ESC ] 8 ; ; ST
var oscPattern = regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`)

// Column defines a table column with title and width
type Column struct {
	Title      string
	Width      int
	NoTruncate bool // If true, this column won't be truncated when scaling
}

// ColumnHint provides configuration hints for a specific column
type ColumnHint struct {
	MaxWidth   int  // Maximum width (0 = no limit)
	NoTruncate bool // If true, this column won't be scaled down
	MinWidth   int  // Minimum width when scaling (0 = use default)
}

// ColumnBuilder helps configure column options using a fluent API
type ColumnBuilder struct {
	hints map[int]ColumnHint
}

// Columns creates a new ColumnBuilder for configuring column options
func Columns() *ColumnBuilder {
	return &ColumnBuilder{hints: make(map[int]ColumnHint)}
}

// NoTruncate marks the specified column indices as non-truncatable
func (b *ColumnBuilder) NoTruncate(indices ...int) *ColumnBuilder {
	for _, i := range indices {
		h := b.hints[i]
		h.NoTruncate = true
		b.hints[i] = h
	}
	return b
}

// MaxWidth sets the maximum width for a specific column
func (b *ColumnBuilder) MaxWidth(index, width int) *ColumnBuilder {
	h := b.hints[index]
	h.MaxWidth = width
	b.hints[index] = h
	return b
}

// MinWidth sets the minimum width for a specific column when scaling
func (b *ColumnBuilder) MinWidth(index, width int) *ColumnBuilder {
	h := b.hints[index]
	h.MinWidth = width
	b.hints[index] = h
	return b
}

// getHint returns the hint for a column index, or an empty hint if none exists
func (b *ColumnBuilder) getHint(index int) ColumnHint {
	if b == nil {
		return ColumnHint{}
	}
	return b.hints[index]
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

// AutoSizeColumns calculates optimal column widths based on content,
// respecting terminal width and column configuration hints.
// Pass nil for builder to use default behavior.
func AutoSizeColumns(headers []string, rows []Row, builder *ColumnBuilder) []Column {
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

	// Track which columns are protected from truncation
	noTruncate := make([]bool, len(headers))
	for i := range headers {
		noTruncate[i] = builder.getHint(i).NoTruncate
	}

	// Apply max widths from hints (skip NoTruncate columns)
	for i := range widths {
		if noTruncate[i] {
			continue // NoTruncate columns ignore MaxWidth
		}
		hint := builder.getHint(i)
		if hint.MaxWidth > 0 {
			widths[i] = min(widths[i], hint.MaxWidth)
		}
	}

	// Only apply terminal width constraints if stdout is a TTY
	if isTTY && termWidth > 0 {
		// Calculate total width needed (including spaces between columns)
		totalWidth := 0
		for _, w := range widths {
			totalWidth += w
		}
		spacing := (len(headers) - 1) * 2 // 2 spaces between each column
		totalWidth += spacing

		// If total width exceeds terminal width, scale down non-protected columns
		if totalWidth > termWidth {
			// Calculate protected width (columns that can't be truncated)
			protectedWidth := 0
			scalableWidth := 0
			for i, w := range widths {
				if noTruncate[i] {
					protectedWidth += w
				} else {
					scalableWidth += w
				}
			}

			// Available width for scalable columns
			availableForScalable := termWidth - spacing - protectedWidth

			// Only scale if there's something to scale and room to do it
			if scalableWidth > 0 && availableForScalable > 0 {
				scaleFactor := float64(availableForScalable) / float64(scalableWidth)

				for i := range widths {
					if noTruncate[i] {
						continue // Don't scale protected columns
					}

					hint := builder.getHint(i)
					minWidth := hint.MinWidth
					if minWidth <= 0 {
						minWidth = 10 // Default minimum
					}

					newWidth := int(float64(widths[i]) * scaleFactor)
					if newWidth < minWidth {
						newWidth = minWidth
					}
					widths[i] = newWidth
				}
			}
		}
	}

	// Create columns
	columns := make([]Column, len(headers))
	for i, header := range headers {
		columns[i] = Column{
			Title:      header,
			Width:      widths[i],
			NoTruncate: noTruncate[i],
		}
	}

	return columns
}

// measureDisplayWidth measures the display width of a string,
// accounting for ANSI escape sequences and OSC 8 hyperlinks
func measureDisplayWidth(s string) int {
	// Strip OSC 8 hyperlink sequences first (lipgloss doesn't handle these)
	s = oscPattern.ReplaceAllString(s, "")
	// lipgloss.Width handles ANSI color sequences correctly
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

		var renderedCell string
		if col.NoTruncate {
			// Don't truncate protected columns
			renderedCell = cellStyle.Render(col.Title)
		} else {
			// Truncate with ellipsis and render
			truncated := runewidth.Truncate(col.Title, col.Width, "…")
			renderedCell = cellStyle.Render(truncated)
		}

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

		// Determine whether to truncate this cell
		var renderedCell string
		if col.NoTruncate {
			// Don't truncate protected columns
			renderedCell = cellStyle.Render(value)
		} else if strings.Contains(value, "\x1b[") || strings.Contains(value, "\x1b]") {
			// Already styled or contains hyperlinks - let lipgloss handle width constraints
			// (runewidth.Truncate doesn't handle ANSI codes or OSC sequences well)
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
