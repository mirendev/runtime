package commands

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"miren.dev/runtime/pkg/color"
	"miren.dev/runtime/pkg/theme"
)

// Colors prints the resolved CLI color theme and a swatch of each semantic role.
// It's a support aid: users on a misdetected terminal can run `miren colors` and
// paste the output so we can see what was detected and how to override it.
func Colors(ctx *Context, opts struct {
}) error {
	ctx.Printf("Resolved theme: %s\n", theme.Current())

	// Query the live terminal background regardless of how the theme was resolved
	// (an override may have skipped auto-detection during startup).
	if bg := color.Background(); bg != "" {
		swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(bg)).Render("████")
		ctx.Printf("Terminal background: %s %s\n", bg, swatch)
	} else {
		ctx.Printf("Terminal background: (terminal did not report)\n")
	}

	ctx.Printf("Color profile: %s\n", profileName(lipgloss.ColorProfile()))
	ctx.Printf("Override with MIREN_THEME=auto|light|dark|no, the `theme` config field, or NO_COLOR/FORCE_COLOR.\n")

	ctx.Printf("\nSemantic roles:\n")
	for _, r := range theme.Roles() {
		swatch := lipgloss.NewStyle().Foreground(r.Color).Render("████")
		ctx.Printf("  %s  %s\n", swatch, r.Name)
	}

	ctx.Printf("\nLane palette (per-service identity):\n")
	for i, c := range theme.Lanes() {
		swatch := lipgloss.NewStyle().Foreground(c).Render("████")
		ctx.Printf("  %s  lane %d\n", swatch, i+1)
	}
	routerSwatch := lipgloss.NewStyle().Foreground(theme.Router).Render("████")
	ctx.Printf("  %s  router (reserved)\n", routerSwatch)

	return nil
}

func profileName(p termenv.Profile) string {
	switch p {
	case termenv.TrueColor:
		return "truecolor"
	case termenv.ANSI256:
		return "ansi256"
	case termenv.ANSI:
		return "ansi"
	case termenv.Ascii:
		return "ascii (no color)"
	default:
		return "unknown"
	}
}
