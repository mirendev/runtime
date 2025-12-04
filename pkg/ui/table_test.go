package ui

import (
	"strings"
	"testing"
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
