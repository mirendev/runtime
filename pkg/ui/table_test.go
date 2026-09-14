package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColumnBuilder(t *testing.T) {
	t.Run("nil builder returns empty hints", func(t *testing.T) {
		var b *ColumnBuilder
		hint := b.getHint(0)
		if hint.NoTruncate {
			t.Error("expected NoTruncate to be false for nil builder")
		}
		if hint.MaxWidth != 0 {
			t.Error("expected MaxWidth to be 0 for nil builder")
		}
	})

	t.Run("NoTruncate sets flag for specified indices", func(t *testing.T) {
		b := Columns().NoTruncate(0, 2)

		if !b.getHint(0).NoTruncate {
			t.Error("expected column 0 to have NoTruncate=true")
		}
		if b.getHint(1).NoTruncate {
			t.Error("expected column 1 to have NoTruncate=false")
		}
		if !b.getHint(2).NoTruncate {
			t.Error("expected column 2 to have NoTruncate=true")
		}
	})

	t.Run("MaxWidth sets width for specified index", func(t *testing.T) {
		b := Columns().MaxWidth(1, 30)

		if b.getHint(0).MaxWidth != 0 {
			t.Error("expected column 0 to have no MaxWidth")
		}
		if b.getHint(1).MaxWidth != 30 {
			t.Errorf("expected column 1 MaxWidth=30, got %d", b.getHint(1).MaxWidth)
		}
	})

	t.Run("MinWidth sets width for specified index", func(t *testing.T) {
		b := Columns().MinWidth(0, 20)

		if b.getHint(0).MinWidth != 20 {
			t.Errorf("expected column 0 MinWidth=20, got %d", b.getHint(0).MinWidth)
		}
	})

	t.Run("chained calls accumulate hints", func(t *testing.T) {
		b := Columns().
			NoTruncate(0).
			MaxWidth(0, 50).
			MinWidth(1, 15)

		hint0 := b.getHint(0)
		if !hint0.NoTruncate {
			t.Error("expected column 0 NoTruncate=true")
		}
		if hint0.MaxWidth != 50 {
			t.Errorf("expected column 0 MaxWidth=50, got %d", hint0.MaxWidth)
		}

		hint1 := b.getHint(1)
		if hint1.MinWidth != 15 {
			t.Errorf("expected column 1 MinWidth=15, got %d", hint1.MinWidth)
		}
	})
}

func TestAutoSizeColumns(t *testing.T) {
	headers := []string{"ID", "NAME", "STATUS"}
	rows := []Row{
		{"abc123", "my-app", "running"},
		{"def456", "another-app", "stopped"},
	}

	t.Run("nil builder uses default behavior", func(t *testing.T) {
		cols := AutoSizeColumns(headers, rows, nil)

		if len(cols) != 3 {
			t.Fatalf("expected 3 columns, got %d", len(cols))
		}

		// All columns should allow truncation
		for i, col := range cols {
			if col.NoTruncate {
				t.Errorf("expected column %d to allow truncation", i)
			}
		}
	})

	t.Run("NoTruncate flag is propagated to columns", func(t *testing.T) {
		cols := AutoSizeColumns(headers, rows, Columns().NoTruncate(0))

		if !cols[0].NoTruncate {
			t.Error("expected column 0 to have NoTruncate=true")
		}
		if cols[1].NoTruncate {
			t.Error("expected column 1 to have NoTruncate=false")
		}
		if cols[2].NoTruncate {
			t.Error("expected column 2 to have NoTruncate=false")
		}
	})

	t.Run("MaxWidth limits column width", func(t *testing.T) {
		cols := AutoSizeColumns(headers, rows, Columns().MaxWidth(1, 5))

		// "another-app" is 11 chars, but should be limited to 5
		if cols[1].Width > 5 {
			t.Errorf("expected column 1 width <= 5, got %d", cols[1].Width)
		}
	})

	t.Run("NoTruncate columns ignore MaxWidth", func(t *testing.T) {
		// If both NoTruncate and MaxWidth are set, NoTruncate takes precedence
		cols := AutoSizeColumns(headers, rows, Columns().NoTruncate(1).MaxWidth(1, 5))

		// "another-app" is 11 chars; NoTruncate should preserve full width
		if cols[1].Width < 11 {
			t.Errorf("expected NoTruncate column to ignore MaxWidth, got width %d", cols[1].Width)
		}
	})

	t.Run("empty headers returns nil", func(t *testing.T) {
		cols := AutoSizeColumns([]string{}, rows, nil)
		if cols != nil {
			t.Error("expected nil for empty headers")
		}
	})

	t.Run("width calculated from content", func(t *testing.T) {
		cols := AutoSizeColumns(headers, rows, nil)

		// "another-app" is the widest value in column 1 (11 chars)
		if cols[1].Width < 11 {
			t.Errorf("expected column 1 width >= 11 (for 'another-app'), got %d", cols[1].Width)
		}
	})
}

// totalWidth is what a row of these columns occupies, spacers included.
func totalWidth(cols []Column) int {
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	return total + (len(cols)-1)*2
}

func TestSizeColumnsNarrowTerminal(t *testing.T) {
	// The shape of 'server operations list': a fixed-width id, a few short
	// columns, a timestamp, and an error that can be arbitrarily long.
	headers := []string{"ID", "ACTION", "PHASE", "VERSIONS", "STARTED", "ERROR"}
	rows := []Row{{
		"01M2GHF1ETZ70A27AW1BP5PNBY", "upgrade", "rolled_back", "main:4be30ed -> main:ce76f6f",
		"2026-09-14 13:04:28", strings.Repeat("server did not report ready ", 8),
	}}
	protect := Columns().NoTruncate(0, 2, 4)

	t.Run("table fits and the error column absorbs the squeeze", func(t *testing.T) {
		cols := sizeColumns(headers, rows, protect, 120)
		if got := totalWidth(cols); got != 120 {
			t.Errorf("expected total width 120, got %d", got)
		}
		for _, i := range []int{0, 2, 4} {
			if cols[i].Width != lipgloss.Width(rows[0][i]) {
				t.Errorf("expected protected column %d at natural width, got %d", i, cols[i].Width)
			}
		}
		if cols[1].Width != 7 {
			t.Errorf("expected ACTION to keep its natural 7, got %d", cols[1].Width)
		}
		if cols[3].Width < 10 {
			t.Errorf("expected VERSIONS at or above the floor, got %d", cols[3].Width)
		}
		if natural := lipgloss.Width(rows[0][5]); cols[5].Width >= natural {
			t.Errorf("expected error column truncated below %d, got %d", natural, cols[5].Width)
		}
	})

	t.Run("protected columns alone fill the terminal", func(t *testing.T) {
		// 26 + 11 + 19 protected plus 5 spacers already exceed 40 columns,
		// so there is nothing to share out. The rest must still sit on
		// their floors rather than stay at natural width.
		cols := sizeColumns(headers, rows, protect, 40)
		want := []int{26, 7, 11, 10, 19, 10}
		for i, w := range want {
			if cols[i].Width != w {
				t.Errorf("column %d: expected width %d, got %d", i, w, cols[i].Width)
			}
		}
		if got := totalWidth(cols); got != 93 {
			t.Errorf("expected total width 93, got %d", got)
		}
	})

	t.Run("floors do not push the total past the terminal", func(t *testing.T) {
		// A single pass would give the long column its proportional share
		// and then bump the short ones up to the floor, overflowing.
		cols := sizeColumns(headers, rows, nil, 100)
		if got := totalWidth(cols); got != 100 {
			t.Errorf("expected total width 100, got %d", got)
		}
	})

	t.Run("no terminal width leaves columns at natural size", func(t *testing.T) {
		cols := sizeColumns(headers, rows, nil, 0)
		if cols[5].Width != lipgloss.Width(rows[0][5]) {
			t.Errorf("expected error column at natural width %d, got %d", lipgloss.Width(rows[0][5]), cols[5].Width)
		}
	})
}

func TestTableRender(t *testing.T) {
	t.Run("empty table returns empty string", func(t *testing.T) {
		table := NewTable()
		if table.Render() != "" {
			t.Error("expected empty string for empty table")
		}
	})

	t.Run("table with columns but no rows returns empty string", func(t *testing.T) {
		table := NewTable(
			WithColumns([]Column{{Title: "ID", Width: 10}}),
		)
		if table.Render() != "" {
			t.Error("expected empty string for table with no rows")
		}
	})

	t.Run("basic table renders", func(t *testing.T) {
		headers := []string{"ID", "NAME"}
		rows := []Row{{"1", "foo"}}
		cols := AutoSizeColumns(headers, rows, nil)

		table := NewTable(
			WithColumns(cols),
			WithRows(rows),
		)

		output := table.Render()
		if output == "" {
			t.Error("expected non-empty output")
		}

		// Should contain header and data
		if !containsText(output, "ID") {
			t.Error("expected output to contain 'ID'")
		}
		if !containsText(output, "NAME") {
			t.Error("expected output to contain 'NAME'")
		}
		if !containsText(output, "foo") {
			t.Error("expected output to contain 'foo'")
		}
	})
}

func containsText(s, text string) bool {
	return text != "" && strings.Contains(s, text)
}
