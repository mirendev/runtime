package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/theme"
)

// Severity selects the label a Diagnostic leads with and the color it wears.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

func (s Severity) label() string {
	if s == SeverityWarning {
		return "WARNING:"
	}
	return "ERROR:"
}

func (s Severity) color() lipgloss.TerminalColor {
	if s == SeverityWarning {
		return theme.Warning
	}
	return theme.Error
}

// Fact is a label/value pair shown in an aligned column, for context the reader
// needs but shouldn't have to parse out of prose (cluster, address, elapsed).
type Fact struct {
	Label string
	Value string
}

// Action is a command the reader can run next, with a short note on what it
// does. Commands are never wrapped, so they stay copy-pasteable.
type Action struct {
	Command string
	Note    string
}

// Diagnostic is a structured, human-facing failure report. It implements
// TerminalError, so returning one from a command yields a rich block on a
// terminal while Error() stays a single line suitable for logs and wrapping.
//
// Both renderers read the same fields, which is the point: a hand-written
// WriteForTerminal alongside a hand-written Error() drifts apart the first time
// someone edits one of them.
//
// Only fill in what you actually know. Every section is omitted when empty, and
// per the doctor design rule, Causes should only list possibilities the reader
// can act on.
type Diagnostic struct {
	// Summary is the one-line headline: what failed, in the reader's terms.
	Summary string
	// Detail is prose explaining what was observed. Wrapped to terminal width.
	Detail string
	// Facts are label/value context pairs, rendered in an aligned column.
	Facts []Fact
	// Causes are possible explanations, most common first. This section only
	// appears when we genuinely don't know which one it is: a diagnosis we're
	// sure of states the cause outright in Detail instead.
	Causes []string
	// Actions are suggested next commands.
	Actions []Action
	// Cause is the underlying error. It is always reachable via errors.Unwrap
	// and included in Error(), but only displayed in the rich block when
	// ShowCause is set, since raw transport errors are noise for most readers.
	Cause error
	// ShowCause displays Cause in the rendered block (wire it to -v).
	ShowCause bool
}

// SeverityTerminalError is a TerminalError that renders its own severity label
// (an "ERROR:"/"WARNING:" prefix, colored to match). The display boundary must
// not add a prefix of its own to these, and should pass the severity it wants
// instead — the same error is an error in one place and a warning in another.
type SeverityTerminalError interface {
	TerminalError
	WriteWithSeverity(w io.Writer, sev Severity)
}

var (
	_ TerminalError         = (*Diagnostic)(nil)
	_ SeverityTerminalError = (*Diagnostic)(nil)
)

// maxDiagnosticWidth caps line length on very wide terminals, where full-width
// prose is harder to read than it is impressive.
const maxDiagnosticWidth = 100

func (d *Diagnostic) Error() string {
	if d.Cause != nil {
		return d.Summary + ": " + d.Cause.Error()
	}
	return d.Summary
}

// Unwrap exposes the underlying error so errors.Is/As still reach it through
// the Diagnostic. Doctor relies on this to re-classify a failure it was handed.
func (d *Diagnostic) Unwrap() error { return d.Cause }

// WriteForTerminal renders the diagnostic at error severity.
func (d *Diagnostic) WriteForTerminal(w io.Writer) {
	d.WriteWithSeverity(w, SeverityError)
}

// WriteWithSeverity renders the diagnostic with an explicit severity, for
// callers that surface the same error type as a non-fatal warning.
func (d *Diagnostic) WriteWithSeverity(w io.Writer, sev Severity) {
	var (
		head    = lipgloss.NewStyle().Foreground(sev.color()).Bold(true)
		section = lipgloss.NewStyle().Foreground(theme.Muted)
		command = lipgloss.NewStyle().Foreground(theme.Info)
		note    = lipgloss.NewStyle().Foreground(theme.Muted)
	)

	fmt.Fprintf(w, "%s %s\n", head.Render(sev.label()), d.Summary)

	// Body content sits at a two-space indent, so it wraps two columns narrower.
	body := diagnosticWidth() - 2

	if d.Detail != "" {
		fmt.Fprintln(w)
		for _, line := range wrapText(d.Detail, body) {
			writeIndented(w, 2, line)
		}
	}

	if len(d.Facts) > 0 {
		fmt.Fprintln(w)
		pad := 0
		for _, f := range d.Facts {
			pad = max(pad, lipgloss.Width(f.Label))
		}
		for _, f := range d.Facts {
			fmt.Fprintf(w, "  %s  %s\n", note.Render(fmt.Sprintf("%-*s", pad, f.Label)), f.Value)
		}
	}

	if len(d.Causes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", section.Render("Possible causes"))
		for _, c := range d.Causes {
			// Bullets hang, so continuation lines align under the text.
			lines := wrapText(c, body-4)
			for i, line := range lines {
				if i == 0 {
					fmt.Fprintf(w, "    %s %s\n", section.Render("•"), line)
				} else {
					writeIndented(w, 6, line)
				}
			}
		}
	}

	if len(d.Actions) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", section.Render("Try"))
		pad := 0
		for _, a := range d.Actions {
			pad = max(pad, lipgloss.Width(a.Command))
		}
		for _, a := range d.Actions {
			if a.Note == "" {
				fmt.Fprintf(w, "    %s\n", command.Render(a.Command))
				continue
			}
			// Pad on the raw command, then style, so the escape sequences
			// don't count toward the column width.
			gap := strings.Repeat(" ", pad-lipgloss.Width(a.Command))
			fmt.Fprintf(w, "    %s%s    %s\n", command.Render(a.Command), gap, note.Render(a.Note))
		}
	}

	if d.ShowCause && d.Cause != nil {
		fmt.Fprintln(w)
		writeIndented(w, 2, note.Render("Cause: "+d.Cause.Error()))
	}
}

func writeIndented(w io.Writer, indent int, line string) {
	if line == "" {
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "%s%s\n", strings.Repeat(" ", indent), line)
}

// diagnosticWidth returns the width to wrap prose at: the terminal's width when
// we have one, 80 when stdout isn't a terminal (pipes, CI logs), capped so wide
// terminals don't produce unreadably long lines.
func diagnosticWidth() int {
	w := TerminalWidth()
	if w <= 0 {
		w = 80
	}
	return min(w, maxDiagnosticWidth)
}

// wrapText word-wraps plain text to width, preserving existing line breaks so
// callers can force a paragraph split. It assumes unstyled input: wrap first,
// style after, or the escape sequences get counted as visible width.
func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}

	var out []string
	for paragraph := range strings.SplitSeq(s, "\n") {
		fields := strings.Fields(paragraph)
		if len(fields) == 0 {
			out = append(out, "")
			continue
		}

		line := fields[0]
		for _, word := range fields[1:] {
			if lipgloss.Width(line)+1+lipgloss.Width(word) > width {
				out = append(out, line)
				line = word
				continue
			}
			line += " " + word
		}
		out = append(out, line)
	}
	return out
}
