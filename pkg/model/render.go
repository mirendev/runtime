package model

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RenderOptions controls the text presentation. It is pure display policy and
// lives on the client; the server only decides how much data to send.
type RenderOptions struct {
	// Expand renders multi-valued and component fields in full instead of
	// collapsing them to a count. A sandbox carries its networks, ports,
	// routes, volumes and containers as repeated components, so a listing is
	// unreadable without this defaulting to off.
	Expand bool

	// Now is the clock used for relative timestamps. Zero means time.Now.
	Now time.Time
}

func (o RenderOptions) now() time.Time {
	if o.Now.IsZero() {
		return time.Now()
	}

	return o.Now
}

// fieldIndent is the column the values line up in. Wide enough for the field
// names the schemas actually use without pushing values off a narrow terminal.
const fieldIndent = 23

// RenderCard writes the detail view of one entity: a header naming it and its
// facets, then one group per facet, then anything unclaimed.
func RenderCard(w io.Writer, doc *Document, opts RenderOptions) {
	fmt.Fprintf(w, "%s", doc.Id)

	if meta := doc.headerMeta(opts.now()); meta != "" {
		fmt.Fprintf(w, "   %s", meta)
	}

	fmt.Fprintln(w)

	// The facet set answers "what is this thing", and for a composed entity it
	// is often the actual question: a sandbox with no schedule facet has not
	// been scheduled yet.
	if len(doc.Kinds) > 0 {
		labels := make([]string, 0, len(doc.Kinds))
		for _, k := range doc.Kinds {
			labels = append(labels, facetLabel(k))
		}

		fmt.Fprintf(w, "facets  %s\n", strings.Join(labels, "  "))
	}

	if doc.Attribute != nil {
		fmt.Fprintln(w)
		renderAttributeView(w, doc.Attribute)
	}

	for _, f := range doc.Facets {
		if len(f.Fields) == 0 {
			continue
		}

		fmt.Fprintf(w, "\n%s\n", f.Label)
		renderFields(w, f.Fields, opts)
	}

	if len(doc.Unclaimed) > 0 {
		fmt.Fprintf(w, "\nunclaimed\n")
		renderFields(w, doc.Unclaimed, opts)
	}

	for _, p := range doc.Problems {
		fmt.Fprintf(w, "\n! %s\n", p)
	}
}

// headerMeta is the dim trailing summary: revision and how long ago it moved.
func (d *Document) headerMeta(now time.Time) string {
	var parts []string

	if d.Revision > 0 {
		parts = append(parts, "rev "+strconv.FormatInt(d.Revision, 10))
	}

	if d.UpdatedAt != nil {
		parts = append(parts, "updated "+relativeTime(*d.UpdatedAt, now))
	}

	if d.ShortId != "" {
		parts = append(parts, d.ShortId)
	}

	return strings.Join(parts, " · ")
}

func renderAttributeView(w io.Writer, av *AttributeView) {
	write := func(name, val string) {
		if val == "" {
			return
		}
		fmt.Fprintf(w, "  %s%s\n", pad(name), val)
	}

	write("type", av.Type)
	write("cardinality", av.Cardinality)
	write("unique", av.Unique)
	write("element type", av.ElementType)

	if av.Indexed {
		write("indexed", "yes")
	}

	if av.Session {
		write("session", "yes")
	}

	write("doc", av.Doc)
}

func renderFields(w io.Writer, fields []Field, opts RenderOptions) {
	for _, f := range fields {
		display, note := renderFieldValue(f, opts)

		fmt.Fprintf(w, "  %s%s", pad(f.Name), display)

		if note != "" {
			fmt.Fprintf(w, "   %s", note)
		}

		fmt.Fprintln(w)
	}
}

// renderFieldValue turns a natural value into one display line plus an optional
// note. Fan-out collapses to a count unless asked to expand, which is what
// keeps an entity with fifty nested components readable.
func renderFieldValue(f Field, opts RenderOptions) (string, string) {
	note := ""
	if f.Truncated {
		note = fmt.Sprintf("%s truncated", humanBytes(f.Size))
	}

	switch v := f.Value.(type) {
	case nil:
		return "-", note

	case []any:
		if len(v) == 0 {
			return "-", note
		}

		// Scalars are short enough to read inline; structures are not.
		if !opts.Expand && containsStructured(v) {
			return fmt.Sprintf("%d %s", len(v), plural(len(v), "component")), "--expand"
		}

		if !opts.Expand {
			return strings.Join(scalarStrings(v), "  "), note
		}

		return "\n" + indentBlock(formatStructured(v), fieldIndent+2), note

	case map[string]any:
		if !opts.Expand {
			return fmt.Sprintf("%d %s", len(v), plural(len(v), "field")), "--expand"
		}

		return "\n" + indentBlock(formatStructured(v), fieldIndent+2), note

	default:
		return fmt.Sprintf("%v", v), note
	}
}

func containsStructured(vs []any) bool {
	for _, v := range vs {
		switch v.(type) {
		case map[string]any, []any:
			return true
		}
	}

	return false
}

func scalarStrings(vs []any) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, fmt.Sprintf("%v", v))
	}

	return out
}

// formatStructured renders nested values as an indented block. Maps are sorted
// so the same entity always renders identically.
func formatStructured(v any) string {
	var b strings.Builder

	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			writeNested(&b, k, t[k])
		}

	case []any:
		for i, elem := range t {
			writeNested(&b, strconv.Itoa(i), elem)
		}

	default:
		fmt.Fprintf(&b, "%v\n", t)
	}

	return b.String()
}

func writeNested(b *strings.Builder, key string, val any) {
	switch val.(type) {
	case map[string]any, []any:
		fmt.Fprintf(b, "%s\n%s", key, indentBlock(formatStructured(val), 2))
	default:
		fmt.Fprintf(b, "%s  %v\n", key, val)
	}
}

func indentBlock(s string, n int) string {
	prefix := strings.Repeat(" ", n)

	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func pad(name string) string {
	if len(name) >= fieldIndent {
		return name + " "
	}

	return name + strings.Repeat(" ", fieldIndent-len(name))
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}

	return word + "s"
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// relativeTime renders an age the way an operator reads it. An absolute
// timestamp is the wrong unit for "did this just change?".
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)

	switch {
	case d < 0:
		return t.Format(time.RFC3339)
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
