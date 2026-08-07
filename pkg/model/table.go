package model

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxDerivedColumns caps how many schema fields become columns. The widest kind
// carries thirteen fields, and a thirteen-column table is not a summary.
const maxDerivedColumns = 4

// TableColumns decides what a listing's columns should be.
//
// Columns cannot come from "the kind", because kinds compose and two entities
// in the same listing may carry different facets. They are derived from the
// facet the caller filtered on, which is the one thing every row shares, and
// only from its scalar single-valued fields: a component or a repeated field
// has no useful one-cell rendering.
//
// explicit overrides the derivation entirely, for when the interesting field is
// not one of the first few.
func TableColumns(docs []*Document, filterKind string, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}

	if filterKind == "" {
		return nil
	}

	// Find the filtered facet on any row that has it; rows differ in their
	// other facets but all of them carry this one.
	var names []string

	seen := make(map[string]bool)

	for _, doc := range docs {
		for _, f := range doc.Facets {
			if f.Kind != filterKind && f.Label != filterKind {
				continue
			}

			for _, fl := range f.Fields {
				if seen[fl.Name] || !columnable(fl) {
					continue
				}

				seen[fl.Name] = true
				names = append(names, fl.Name)
			}
		}
	}

	if len(names) > maxDerivedColumns {
		names = names[:maxDerivedColumns]
	}

	return names
}

// maxColumnWidth is how wide a value may be before it stops working as a
// column. A base64 blob is technically a scalar but makes a useless one.
const maxColumnWidth = 24

// columnable reports whether a field reads well as one cell.
func columnable(f Field) bool {
	if !isScalar(f.Value) {
		return false
	}

	// Bytes render as base64, which is never a useful column no matter how
	// short this particular value happens to be.
	if f.Type == "bytes" {
		return false
	}

	return utf8.RuneCountInString(fmt.Sprintf("%v", f.Value)) <= maxColumnWidth
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, map[string]any, []any:
		return false
	default:
		return true
	}
}

// RenderTable writes a listing as one row per entity. It is the scanning view;
// RenderCard is the reading view.
func RenderTable(w io.Writer, docs []*Document, columns []string, opts RenderOptions) {
	headers := append([]string{"ID", "FACETS"}, upperAll(columns)...)
	headers = append(headers, "REV", "UPDATED")

	rows := make([][]string, 0, len(docs))

	for _, doc := range docs {
		row := []string{doc.rowId(), doc.facetSummary()}

		for _, c := range columns {
			row = append(row, doc.columnValue(c))
		}

		rev := "-"
		if doc.Revision > 0 {
			rev = strconv.FormatInt(doc.Revision, 10)
		}

		updated := "-"
		if doc.UpdatedAt != nil {
			updated = relativeTime(*doc.UpdatedAt, opts.now())
		}

		rows = append(rows, append(row, rev, updated))
	}

	writeAligned(w, headers, rows)
}

// rowId prefers the short id, which is what an operator will type back.
func (d *Document) rowId() string {
	if d.ShortId != "" {
		return d.ShortId
	}

	return d.Id
}

// facetSummary lists the facets minus the near-universal metadata mixin, which
// is on almost everything and so distinguishes nothing.
func (d *Document) facetSummary() string {
	var labels []string

	for _, k := range d.Kinds {
		label := facetLabel(k)
		if label == "core/metadata" {
			continue
		}

		labels = append(labels, label)
	}

	if len(labels) == 0 {
		if d.Attribute != nil {
			return "attribute"
		}

		return "-"
	}

	return strings.Join(labels, "+")
}

// columnValue finds a field by name across every facet, so a column derived
// from one facet still resolves for a row that carries it under another.
func (d *Document) columnValue(name string) string {
	for _, f := range d.Facets {
		for _, fl := range f.Fields {
			if fl.Name != name {
				continue
			}

			if !isScalar(fl.Value) {
				return summarize(fl.Value)
			}

			return fmt.Sprintf("%v", fl.Value)
		}
	}

	return "-"
}

func summarize(v any) string {
	switch t := v.(type) {
	case []any:
		return fmt.Sprintf("%d %s", len(t), plural(len(t), "item"))
	case map[string]any:
		return fmt.Sprintf("%d %s", len(t), plural(len(t), "field"))
	default:
		return "-"
	}
}

func upperAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, strings.ToUpper(s))
	}

	return out
}

// writeAligned pads columns to a common width. The ui table helpers are built
// for bounded terminal tables; entity ids and facet lists are long enough that
// a plain aligned dump stays more readable and pipes cleanly.
func writeAligned(w io.Writer, headers []string, rows [][]string) {
	// Runes, not bytes: a cell holding non-ASCII (or the ellipsis a truncated
	// value ends in) occupies fewer columns than it does bytes, and measuring
	// bytes pads it into misalignment.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			if w := utf8.RuneCountInString(cell); i < len(widths) && w > widths[i] {
				widths[i] = w
			}
		}
	}

	writeRow(w, headers, widths)

	for _, row := range rows {
		writeRow(w, row, widths)
	}
}

func writeRow(w io.Writer, cells []string, widths []int) {
	var b strings.Builder

	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}

		b.WriteString(cell)

		// No trailing padding on the last column, so the line has no ragged
		// whitespace when piped.
		if i < len(cells)-1 && i < len(widths) {
			b.WriteString(strings.Repeat(" ", max(0, widths[i]-utf8.RuneCountInString(cell))))
		}
	}

	fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
}
