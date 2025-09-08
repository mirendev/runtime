package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// DisplayStatus returns a colored version of the status string based on common status values.
// It also removes the "status." prefix if present.
func DisplayStatus(status string) string {
	// Remove "status." prefix if present
	status = strings.TrimPrefix(status, "status.")

	// Apply color based on status value
	switch status {
	case "dead", "failed", "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(status) // gray
	case "running", "active", "healthy", "ready":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(status) // green
	case "stopped", "inactive", "unhealthy", "not_ready":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(status) // red
	case "pending", "starting", "waiting", "creating":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(status) // blue
	case "paused", "suspended":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(status) // yellow
	default:
		return status // no color for unknown/other statuses
	}
}

// CleanStatus removes the "status." prefix from a status string without applying color.
func CleanStatus(status string) string {
	return strings.TrimPrefix(status, "status.")
}
