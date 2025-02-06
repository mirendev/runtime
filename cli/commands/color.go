package commands

import (
	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/pkg/color"
)

func Colors(ctx *Context, opts struct {
}) error {

	sample := func(str string, color string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(str)
	}
	bg := color.Background()
	lf := color.LiveFaint()

	ctx.Printf("Background: %s %s\n", bg, sample("background", bg))
	ctx.Printf("LiveFaint: %s %s\n", lf, sample("faint background", lf))
	ctx.Printf("ANSIFaint: %s\n",
		lipgloss.NewStyle().Faint(true).Render("ansi faint"))
	return nil
}
