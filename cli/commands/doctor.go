package commands

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

var (
	infoGreen  = lipgloss.NewStyle().Foreground(theme.Success)
	infoRed    = lipgloss.NewStyle().Foreground(theme.Error)
	infoYellow = lipgloss.NewStyle().Foreground(theme.Warning)
	infoGray   = lipgloss.NewStyle().Foreground(theme.Muted)
	infoLabel  = lipgloss.NewStyle().Foreground(theme.Info)
	infoBold   = lipgloss.NewStyle().Bold(true)
)

// Doctor runs the full diagnostic sweep.
//
// It is deliberately a single command. The previous version printed a
// three-line summary and then told you to go read three other commands, which
// made the summary a menu rather than a diagnosis. Everything runs here.
func Doctor(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	env := gatherDoctorEnv(ctx, opts.ConfigCentric)

	checks := doctorChecks()
	results := make([]checkResult, len(checks))
	for i, c := range checks {
		results[i] = c.Run(env)
	}

	// Set before rendering, so it applies to every output format. JSON is the
	// form a script is most likely to consume, and scripted health gates are
	// the whole reason the exit code exists — having it apply only to the
	// human-readable output would defeat the point.
	//
	// A non-zero exit counts outright failures only: warnings are advisory by
	// definition, and exiting non-zero for them would make the signal useless.
	for _, r := range results {
		if r.Status == checkFail {
			ctx.SetExitCode(1)
			break
		}
	}

	if opts.IsJSON() {
		return printDoctorJSON(checks, results)
	}

	renderDoctor(ctx, checks, results)

	return nil
}

func renderDoctor(ctx *Context, checks []check, results []checkResult) {
	ctx.Printf("%s\n\n", infoBold.Render("Miren Doctor"))

	width := 0
	for _, c := range checks {
		width = max(width, len(c.Name))
	}

	for i, c := range checks {
		ctx.Printf("  %s %s  %s\n",
			statusMark(results[i].Status),
			fmt.Sprintf("%-*s", width, c.Name),
			statusText(results[i]))
	}

	fails, warns := 0, 0
	for _, r := range results {
		switch r.Status {
		case checkFail:
			fails++
		case checkWarn:
			warns++
		case checkOK, checkSkip:
		}
	}

	if fails == 0 && warns == 0 {
		ctx.Printf("\n%s\n", "Everything looks good.")
		return
	}

	// Only failing checks explain themselves. Everything that's fine already
	// said so in one line above.
	for _, r := range results {
		if r.Problem == nil {
			continue
		}
		ctx.Printf("\n")
		severity := ui.SeverityError
		if r.Status == checkWarn {
			severity = ui.SeverityWarning
		}
		r.Problem.ShowCause = ctx.Verbose()
		r.Problem.WriteWithSeverity(ctx.Stdout, severity)
	}

	ctx.Printf("\n%s\n", doctorFooter(fails, warns))
}

// doctorFooter keeps the wording honest about severity. Counting a warning as a
// "problem" while exiting zero tells the reader two different things at once.
func doctorFooter(fails, warns int) string {
	switch {
	case fails == 0 && warns == 0:
		return "Everything looks good."
	case fails == 0:
		return fmt.Sprintf("%s, nothing broken.", countOf(warns, "warning"))
	case warns == 0:
		return fmt.Sprintf("%s found.", countOf(fails, "problem"))
	default:
		return fmt.Sprintf("%s found, %s.", countOf(fails, "problem"), countOf(warns, "warning"))
	}
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func statusMark(s checkStatus) string {
	switch s {
	case checkOK:
		return infoGreen.Render("[" + ui.Checkmark + "]")
	case checkWarn:
		return infoYellow.Render("[!]")
	case checkFail:
		return infoRed.Render("[✗]")
	case checkSkip:
		return infoGray.Render("[-]")
	default:
		return infoGray.Render("[-]")
	}
}

func statusText(r checkResult) string {
	if r.Status == checkSkip {
		return infoGray.Render(r.Summary)
	}
	return r.Summary
}

func printDoctorJSON(checks []check, results []checkResult) error {
	type checkJSON struct {
		Name    string   `json:"name"`
		Status  string   `json:"status"`
		Summary string   `json:"summary"`
		Problem string   `json:"problem,omitempty"`
		Actions []string `json:"actions,omitempty"`
	}

	items := make([]checkJSON, len(checks))
	for i, c := range checks {
		item := checkJSON{
			Name:    c.Name,
			Status:  results[i].Status.String(),
			Summary: results[i].Summary,
		}
		if p := results[i].Problem; p != nil {
			item.Problem = p.Summary
			for _, a := range p.Actions {
				item.Actions = append(item.Actions, a.Command)
			}
		}
		items[i] = item
	}

	return PrintJSON(items)
}
